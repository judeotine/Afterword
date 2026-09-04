//go:build integration

package api_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/payments"
)

func (h *billingHarness) auditActions(workspaceID uuid.UUID) []string {
	h.t.Helper()
	rows, err := h.pool.Query(h.context(),
		`SELECT actor || ' ' || action || ' ' || target FROM audit_log WHERE workspace_id = $1 ORDER BY at`, workspaceID)
	if err != nil {
		h.t.Fatalf("read the audit log: %v", err)
	}
	defer rows.Close()

	var entries []string
	for rows.Next() {
		var entry string
		if err := rows.Scan(&entry); err != nil {
			h.t.Fatalf("scan an audit entry: %v", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("iterate the audit log: %v", err)
	}
	return entries
}

func (h *billingHarness) auditActor(workspaceID uuid.UUID) string {
	h.t.Helper()
	var actor string
	if err := h.pool.QueryRow(h.context(),
		`SELECT actor FROM audit_log WHERE workspace_id = $1 ORDER BY at LIMIT 1`, workspaceID).Scan(&actor); err != nil {
		h.t.Fatalf("read the audit actor: %v", err)
	}
	return actor
}

func (h *billingHarness) paymentStatus(paymentID string) string {
	h.t.Helper()
	var status string
	if err := h.pool.QueryRow(h.context(),
		`SELECT status FROM payments WHERE id = $1`, paymentID).Scan(&status); err != nil {
		h.t.Fatalf("read the payment status: %v", err)
	}
	return status
}

func (h *billingHarness) deliverFakeAmount(paymentID string, amountMinor int64, currency string) response {
	h.t.Helper()
	return h.do(http.MethodPost, fakeWebhookURL, map[string]any{
		"payment_id":   paymentID,
		"status":       "paid",
		"amount_minor": amountMinor,
		"currency":     currency,
	}, withFakeSignature(billingFakeSecret))
}

func TestAWebhookThatReportsNoAmountIsHeldForReview(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("amount-missing@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 100)
	created := h.checkout(session, packID, "")

	silent := h.do(http.MethodPost, fakeWebhookURL,
		map[string]string{"payment_id": created.PaymentID}, withFakeSignature(billingFakeSecret))
	if silent.Status != http.StatusOK {
		t.Fatalf("amountless webhook: status %d, body %s", silent.Status, silent.Body)
	}
	var payload webhookPayload
	silent.decode(t, &payload)
	if !payload.NeedsReview {
		t.Fatalf("amountless webhook = %+v, want needs_review", payload)
	}
	if got := h.balance(workspaceID); got != 0 {
		t.Fatalf("a webhook that reported no amount credited %d minutes", got)
	}
}

func TestAWebhookWhoseAmountDisagreesIsHeldForReview(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("amount-mismatch@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 500)
	created := h.checkout(session, packID, "")

	mismatched := h.deliverFakeAmount(created.PaymentID, 6000, "UGX")
	if mismatched.Status != http.StatusOK {
		t.Fatalf("mismatched webhook: status %d, body %s", mismatched.Status, mismatched.Body)
	}
	var payload webhookPayload
	mismatched.decode(t, &payload)
	if !payload.NeedsReview || payload.Duplicate {
		t.Fatalf("mismatched webhook = %+v, want needs_review", payload)
	}
	if got := h.balance(workspaceID); got != 0 {
		t.Fatalf("balance after a mismatched amount = %d, want 0", got)
	}
	if got := h.paymentStatus(created.PaymentID); got != "needs_review" {
		t.Fatalf("payment status = %q, want needs_review", got)
	}

	entries := h.auditActions(workspaceID)
	if len(entries) != 1 || !strings.Contains(entries[0], "billing.payment.needs_review") {
		t.Fatalf("audit log = %+v", entries)
	}

	replayed := h.deliverFakeAmount(created.PaymentID, 60000, "UGX")
	if replayed.Status != http.StatusOK {
		t.Fatalf("replayed webhook: status %d, body %s", replayed.Status, replayed.Body)
	}
	if got := h.balance(workspaceID); got != 0 {
		t.Fatalf("a payment held for review was credited later: balance %d", got)
	}
}

func TestAWebhookMatchingTheStoredAmountStillSettles(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("amount-match@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 500)
	created := h.checkout(session, packID, "")

	settled := h.deliverFakeAmount(created.PaymentID, 60000, "ugx")
	if settled.Status != http.StatusOK {
		t.Fatalf("matching webhook: status %d, body %s", settled.Status, settled.Body)
	}
	var payload webhookPayload
	settled.decode(t, &payload)
	if payload.NeedsReview || payload.Status != string(payments.StatusPaid) {
		t.Fatalf("matching webhook = %+v", payload)
	}
	if got := h.balance(workspaceID); got != 500 {
		t.Fatalf("balance = %d, want 500", got)
	}
}

func TestALateWebhookOnAReapedPaymentIsHeldForReview(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("late-webhook@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 100)
	created := h.checkout(session, packID, "")

	if _, err := h.pool.Exec(h.context(),
		`UPDATE payments SET created_at = now() - interval '48 hours' WHERE id = $1`, created.PaymentID); err != nil {
		t.Fatalf("age the payment: %v", err)
	}
	if _, err := h.service.ReapPendingPayments(h.context(), 24*time.Hour); err != nil {
		t.Fatalf("reap: %v", err)
	}
	if got := h.paymentStatus(created.PaymentID); got != string(payments.StatusFailed) {
		t.Fatalf("payment status after the reaper = %q, want failed", got)
	}

	late := h.deliverFakeWebhook(created.PaymentID, 15000)
	if late.Status != http.StatusOK {
		t.Fatalf("late webhook: status %d, body %s", late.Status, late.Body)
	}
	var payload webhookPayload
	late.decode(t, &payload)
	if !payload.NeedsReview {
		t.Fatalf("late webhook = %+v, want needs_review", payload)
	}
	if got := h.balance(workspaceID); got != 0 {
		t.Fatalf("a late webhook credited a failed payment: balance %d", got)
	}
	if got := h.paymentStatus(created.PaymentID); got != "needs_review" {
		t.Fatalf("payment status = %q, want needs_review", got)
	}
	entries := h.auditActions(workspaceID)
	if len(entries) != 1 || !strings.Contains(entries[0], "billing.payment.needs_review") {
		t.Fatalf("audit log = %+v", entries)
	}
}

func TestCheckoutIsRateLimitedPerWorkspace(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("checkout-flood@example.com")
	packID := h.packWithMinutes(session.AccessToken, 100)

	for i := range 10 {
		got := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": packID},
			withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
		if got.Status != http.StatusCreated {
			t.Fatalf("checkout %d: status %d, body %s", i, got.Status, got.Body)
		}
	}

	limited := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": packID},
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if limited.Status != http.StatusTooManyRequests {
		t.Fatalf("the eleventh checkout: status %d, body %s", limited.Status, limited.Body)
	}
	if limited.Header.Get("Retry-After") == "" {
		t.Fatal("the rate limited checkout carried no Retry-After header")
	}
	if code := limited.errorCode(t); code != "rate_limited" {
		t.Fatalf("error code = %q", code)
	}

	other := h.signIn("checkout-neighbour@example.com")
	neighbour := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": packID},
		withBearer(other.AccessToken), withWorkspace(other.Workspace.ID))
	if neighbour.Status != http.StatusCreated {
		t.Fatalf("another workspace was limited too: status %d, body %s", neighbour.Status, neighbour.Body)
	}
}

func TestCheckoutNormalisesThePhoneNumber(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("phone-normalise@example.com")
	packID := h.packWithMinutes(session.AccessToken, 100)

	created := h.checkout(session, packID, "+256 700 000 000")
	if !created.PushSent {
		t.Fatal("a checkout with a phone number reported no push")
	}
	started := h.fake.Started()
	if len(started) != 1 || started[0].Phone != "+256700000000" {
		t.Fatalf("the provider was asked for %+v, want a normalised phone", started)
	}

	rejected := h.do(http.MethodPost, checkoutURL,
		map[string]string{"pack_id": packID, "phone": "not-a-phone"},
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if rejected.Status != http.StatusBadRequest {
		t.Fatalf("checkout with an unusable phone: status %d, body %s", rejected.Status, rejected.Body)
	}
}

func TestAdminAdjustmentsAreAudited(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("audited-adjust@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)

	granted := h.do(http.MethodPost, adjustURL(session.Workspace.ID),
		map[string]any{"delta_minutes": 250, "ref_id": "ticket-77"}, withAdminToken(billingAdminToken))
	if granted.Status != http.StatusOK {
		t.Fatalf("adjust: status %d, body %s", granted.Status, granted.Body)
	}

	entries := h.auditActions(workspaceID)
	if len(entries) != 1 {
		t.Fatalf("audit log = %+v, want one entry", entries)
	}
	if actor := h.auditActor(workspaceID); actor != "admin-token" {
		t.Fatalf("audit actor = %q, want admin-token", actor)
	}
	for _, want := range []string{"billing.credits.adjust", "250", "ticket-77"} {
		if !strings.Contains(entries[0], want) {
			t.Fatalf("audit entry %q does not mention %q", entries[0], want)
		}
	}

	refused := h.do(http.MethodPost, adjustURL(session.Workspace.ID),
		map[string]any{"delta_minutes": -5000}, withAdminToken(billingAdminToken))
	if refused.Status != http.StatusPaymentRequired {
		t.Fatalf("overdrawing adjust: status %d", refused.Status)
	}
	if entries := h.auditActions(workspaceID); len(entries) != 1 {
		t.Fatalf("a refused adjustment was audited: %+v", entries)
	}
}
