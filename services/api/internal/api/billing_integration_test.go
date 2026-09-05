//go:build integration

package api_test

import (
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/payments"
)

const (
	packsURL        = "/v1/billing/packs"
	balanceURL      = "/v1/billing/balance"
	checkoutURL     = "/v1/billing/checkout"
	paymentsURL     = "/v1/billing/payments"
	fakeWebhookURL  = "/v1/billing/webhooks/fake"
	nylonWebhookURL = "/v1/billing/webhooks/nylonpay"
)

func adjustURL(workspaceID string) string {
	return "/v1/admin/workspaces/" + workspaceID + "/credits/adjust"
}

func (h *billingHarness) packs(token string) packsPayload {
	h.t.Helper()
	response := h.do(http.MethodGet, packsURL, nil, withBearer(token))
	if response.Status != http.StatusOK {
		h.t.Fatalf("list packs: status %d, body %s", response.Status, response.Body)
	}
	var payload packsPayload
	response.decode(h.t, &payload)
	return payload
}

func (h *billingHarness) packWithMinutes(token string, minutes int32) string {
	h.t.Helper()
	for _, pack := range h.packs(token).Packs {
		if pack.Minutes == minutes {
			return pack.ID
		}
	}
	h.t.Fatalf("no seeded pack has %d minutes", minutes)
	return ""
}

func (h *billingHarness) readBalance(session session) balancePayload {
	h.t.Helper()
	response := h.do(http.MethodGet, balanceURL, nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if response.Status != http.StatusOK {
		h.t.Fatalf("read balance: status %d, body %s", response.Status, response.Body)
	}
	var payload balancePayload
	response.decode(h.t, &payload)
	return payload
}

func (h *billingHarness) checkout(session session, packID, phone string) checkoutPayload {
	h.t.Helper()
	body := map[string]string{"pack_id": packID}
	if phone != "" {
		body["phone"] = phone
	}
	response := h.do(http.MethodPost, checkoutURL, body,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if response.Status != http.StatusCreated {
		h.t.Fatalf("checkout: status %d, body %s", response.Status, response.Body)
	}
	var payload checkoutPayload
	response.decode(h.t, &payload)
	return payload
}

func (h *billingHarness) deliverFakeWebhook(paymentID string, amountMinor int64) response {
	h.t.Helper()
	return h.do(http.MethodPost, fakeWebhookURL, map[string]any{
		"payment_id":   paymentID,
		"amount_minor": amountMinor,
		"currency":     "UGX",
	}, withFakeSignature(billingFakeSecret))
}

func TestSeededPacksAreListedCheapestFirst(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("packs@example.com")

	anonymous := h.do(http.MethodGet, packsURL, nil)
	if anonymous.Status != http.StatusUnauthorized {
		t.Fatalf("packs without a token = %d, want 401", anonymous.Status)
	}

	payload := h.packs(session.AccessToken)
	if len(payload.Packs) != 3 {
		t.Fatalf("seeded packs = %d, want 3", len(payload.Packs))
	}

	wantMinutes := []int32{100, 500, 2000}
	wantPrices := []int64{15000, 60000, 200000}
	for i, pack := range payload.Packs {
		if pack.Minutes != wantMinutes[i] {
			t.Fatalf("pack %d minutes = %d, want %d", i, pack.Minutes, wantMinutes[i])
		}
		if pack.PriceMinor != wantPrices[i] {
			t.Fatalf("pack %d price = %d, want %d", i, pack.PriceMinor, wantPrices[i])
		}
		if pack.Currency != "UGX" {
			t.Fatalf("pack %d currency = %q, want UGX", i, pack.Currency)
		}
	}
}

func TestBalanceReportsTheLedgerSumAndTheGrantExpiry(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("balance@example.com")

	empty := h.readBalance(session)
	if empty.Minutes != 0 {
		t.Fatalf("starting balance = %d, want 0", empty.Minutes)
	}
	if empty.FreeGrantMinutes != 300 {
		t.Fatalf("free grant = %d, want 300", empty.FreeGrantMinutes)
	}
	if empty.GrantExpiresAt != nil {
		t.Fatalf("grant expiry before any grant = %v, want null", *empty.GrantExpiresAt)
	}

	granter, err := credits.NewGranter(credits.GranterOptions{Pool: h.pool, Ledger: h.ledger, Minutes: 300})
	if err != nil {
		t.Fatalf("new granter: %v", err)
	}
	if _, err := granter.RunOnce(h.context()); err != nil {
		t.Fatalf("run the granter: %v", err)
	}

	granted := h.readBalance(session)
	if granted.Minutes != 300 {
		t.Fatalf("balance after the grant = %d, want 300", granted.Minutes)
	}
	if granted.GrantExpiresAt == nil {
		t.Fatal("balance after the grant carries no expiry")
	}
}

func TestBalanceIsRefusedWithoutAWorkspace(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("scoped@example.com")

	missing := h.do(http.MethodGet, balanceURL, nil, withBearer(session.AccessToken))
	if missing.Status != http.StatusBadRequest && missing.Status != http.StatusForbidden {
		t.Fatalf("balance without a workspace header = %d, want 400 or 403", missing.Status)
	}

	other := h.signIn("other-workspace@example.com")
	crossed := h.do(http.MethodGet, balanceURL, nil,
		withBearer(session.AccessToken), withWorkspace(other.Workspace.ID))
	if crossed.Status != http.StatusForbidden {
		t.Fatalf("balance across workspaces = %d, want 403", crossed.Status)
	}
}

func TestCheckoutThenTheFakeWebhookCreditsTheWorkspaceExactlyOnce(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("checkout@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 500)

	created := h.checkout(session, packID, "+256700000000")
	if created.Status != string(payments.StatusPending) {
		t.Fatalf("checkout status = %q, want pending", created.Status)
	}
	if !created.PushSent {
		t.Fatal("checkout with a phone number did not report a push")
	}
	if created.Minutes != 500 || created.AmountMinor != 60000 || created.Currency != "UGX" {
		t.Fatalf("checkout = %+v, want the 500 minute UGX pack", created)
	}
	if h.balance(workspaceID) != 0 {
		t.Fatalf("balance before the webhook = %d, want 0", h.balance(workspaceID))
	}

	first := h.deliverFakeWebhook(created.PaymentID, 60000)
	if first.Status != http.StatusOK {
		t.Fatalf("first webhook: status %d, body %s", first.Status, first.Body)
	}
	var firstPayload webhookPayload
	first.decode(t, &firstPayload)
	if firstPayload.Status != string(payments.StatusPaid) || firstPayload.Duplicate {
		t.Fatalf("first webhook = %+v, want paid and not a duplicate", firstPayload)
	}
	if got := h.balance(workspaceID); got != 500 {
		t.Fatalf("balance after the first webhook = %d, want 500", got)
	}

	second := h.deliverFakeWebhook(created.PaymentID, 60000)
	if second.Status != http.StatusOK {
		t.Fatalf("second webhook: status %d, body %s", second.Status, second.Body)
	}
	var secondPayload webhookPayload
	second.decode(t, &secondPayload)
	if !secondPayload.Duplicate || secondPayload.Status != string(payments.StatusPaid) {
		t.Fatalf("second webhook = %+v, want paid and a duplicate", secondPayload)
	}
	if got := h.balance(workspaceID); got != 500 {
		t.Fatalf("balance after the duplicate webhook = %d, want 500", got)
	}

	history := h.do(http.MethodGet, paymentsURL, nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if history.Status != http.StatusOK {
		t.Fatalf("list payments: status %d, body %s", history.Status, history.Body)
	}
	var listed paymentsPayload
	history.decode(t, &listed)
	if len(listed.Payments) != 1 {
		t.Fatalf("payment history = %d rows, want 1", len(listed.Payments))
	}
	if listed.Payments[0].Status != string(payments.StatusPaid) || listed.Payments[0].Minutes != 500 {
		t.Fatalf("payment history row = %+v, want a paid 500 minute row", listed.Payments[0])
	}
}

func TestConcurrentWebhookDeliveriesCreditTheWorkspaceOnce(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("concurrent-webhook@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	packID := h.packWithMinutes(session.AccessToken, 100)

	created := h.checkout(session, packID, "")
	if created.RedirectURL == "" {
		t.Fatal("checkout without a phone number returned no redirect url")
	}

	const deliveries = 6
	results := make([]response, deliveries)
	var wait sync.WaitGroup
	for i := range results {
		wait.Add(1)
		go func() {
			defer wait.Done()
			results[i] = h.deliverFakeWebhook(created.PaymentID, 15000)
		}()
	}
	wait.Wait()

	credited := 0
	for _, result := range results {
		if result.Status != http.StatusOK {
			t.Fatalf("concurrent webhook: status %d, body %s", result.Status, result.Body)
		}
		var payload webhookPayload
		result.decode(t, &payload)
		if !payload.Duplicate {
			credited++
		}
	}
	if credited != 1 {
		t.Fatalf("%d deliveries reported a fresh credit, want 1", credited)
	}
	if got := h.balance(workspaceID); got != 100 {
		t.Fatalf("balance after %d concurrent deliveries = %d, want 100", deliveries, got)
	}
}

func TestCheckoutRejectsUnknownAndMalformedPacks(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("bad-checkout@example.com")

	unknown := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": uuid.NewString()},
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if unknown.Status != http.StatusNotFound {
		t.Fatalf("checkout with an unknown pack = %d, want 404", unknown.Status)
	}

	malformed := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": "not-a-uuid"},
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if malformed.Status != http.StatusBadRequest {
		t.Fatalf("checkout with a malformed pack id = %d, want 400", malformed.Status)
	}
}

func TestWebhooksAreRefusedWithoutAValidSecretOrProvider(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("webhook-guard@example.com")
	packID := h.packWithMinutes(session.AccessToken, 100)
	created := h.checkout(session, packID, "")

	unsigned := h.do(http.MethodPost, fakeWebhookURL, map[string]string{"payment_id": created.PaymentID})
	if unsigned.Status != http.StatusUnauthorized {
		t.Fatalf("unsigned webhook = %d, want 401", unsigned.Status)
	}
	if code := unsigned.errorCode(t); code != "signature_invalid" {
		t.Fatalf("unsigned webhook code = %q, want signature_invalid", code)
	}

	wrongSecret := h.do(http.MethodPost, fakeWebhookURL,
		map[string]string{"payment_id": created.PaymentID}, withFakeSignature("not-the-secret"))
	if wrongSecret.Status != http.StatusUnauthorized {
		t.Fatalf("badly signed webhook = %d, want 401", wrongSecret.Status)
	}

	unknownProvider := h.do(http.MethodPost, nylonWebhookURL,
		map[string]string{"payment_id": created.PaymentID}, withFakeSignature(billingFakeSecret))
	if unknownProvider.Status != http.StatusNotFound {
		t.Fatalf("webhook for an unregistered provider = %d, want 404", unknownProvider.Status)
	}

	unknownPayment := h.deliverFakeWebhook(uuid.NewString(), 15000)
	if unknownPayment.Status != http.StatusAccepted {
		t.Fatalf("webhook for an unknown payment = %d, want 202", unknownPayment.Status)
	}

	if h.balance(uuid.MustParse(session.Workspace.ID)) != 0 {
		t.Fatal("a refused webhook moved the balance")
	}
}

func TestAdminAdjustNeedsTheAdminTokenAndMovesTheBalance(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("admin-adjust@example.com")
	workspaceID := session.Workspace.ID

	unauthenticated := h.do(http.MethodPost, adjustURL(workspaceID), map[string]any{"delta_minutes": 120})
	if unauthenticated.Status != http.StatusUnauthorized {
		t.Fatalf("adjust without a token = %d, want 401", unauthenticated.Status)
	}

	wrongToken := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": 120}, withAdminToken("wrong-token-0123456789abcdefghij"))
	if wrongToken.Status != http.StatusUnauthorized {
		t.Fatalf("adjust with the wrong token = %d, want 401", wrongToken.Status)
	}

	userToken := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": 120}, withAdminToken(session.AccessToken))
	if userToken.Status != http.StatusUnauthorized {
		t.Fatalf("adjust with a user access token = %d, want 401", userToken.Status)
	}

	granted := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": 120, "ref_id": "support-ticket-42"}, withAdminToken(billingAdminToken))
	if granted.Status != http.StatusOK {
		t.Fatalf("adjust: status %d, body %s", granted.Status, granted.Body)
	}
	var afterGrant balancePayload
	granted.decode(t, &afterGrant)
	if afterGrant.Minutes != 120 {
		t.Fatalf("balance after the adjustment = %d, want 120", afterGrant.Minutes)
	}

	taken := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": -20}, withAdminToken(billingAdminToken))
	if taken.Status != http.StatusOK {
		t.Fatalf("negative adjust: status %d, body %s", taken.Status, taken.Body)
	}
	var afterTake balancePayload
	taken.decode(t, &afterTake)
	if afterTake.Minutes != 100 {
		t.Fatalf("balance after the negative adjustment = %d, want 100", afterTake.Minutes)
	}

	overdrawn := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": -500}, withAdminToken(billingAdminToken))
	if overdrawn.Status != http.StatusPaymentRequired {
		t.Fatalf("overdrawing adjust = %d, want 402", overdrawn.Status)
	}
	if code := overdrawn.errorCode(t); code != "insufficient_credits" {
		t.Fatalf("overdrawing adjust code = %q, want insufficient_credits", code)
	}

	zero := h.do(http.MethodPost, adjustURL(workspaceID),
		map[string]any{"delta_minutes": 0}, withAdminToken(billingAdminToken))
	if zero.Status != http.StatusBadRequest {
		t.Fatalf("zero adjust = %d, want 400", zero.Status)
	}

	unknownWorkspace := h.do(http.MethodPost, adjustURL(uuid.NewString()),
		map[string]any{"delta_minutes": 10}, withAdminToken(billingAdminToken))
	if unknownWorkspace.Status != http.StatusNotFound {
		t.Fatalf("adjust on an unknown workspace = %d, want 404", unknownWorkspace.Status)
	}

	if got := h.balance(uuid.MustParse(workspaceID)); got != 100 {
		t.Fatalf("final balance = %d, want 100", got)
	}
}

func TestPaymentHistoryPagesWithACursor(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("history@example.com")
	packID := h.packWithMinutes(session.AccessToken, 100)

	for range 3 {
		h.checkout(session, packID, "")
	}

	first := h.do(http.MethodGet, paymentsURL+"?limit=2", nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if first.Status != http.StatusOK {
		t.Fatalf("first page: status %d, body %s", first.Status, first.Body)
	}
	var firstPage paymentsPayload
	first.decode(t, &firstPage)
	if len(firstPage.Payments) != 2 || firstPage.NextCursor == "" {
		t.Fatalf("first page = %d rows, cursor %q, want 2 rows and a cursor", len(firstPage.Payments), firstPage.NextCursor)
	}

	second := h.do(http.MethodGet, paymentsURL+"?limit=2&cursor="+firstPage.NextCursor, nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if second.Status != http.StatusOK {
		t.Fatalf("second page: status %d, body %s", second.Status, second.Body)
	}
	var secondPage paymentsPayload
	second.decode(t, &secondPage)
	if len(secondPage.Payments) != 1 || secondPage.NextCursor != "" {
		t.Fatalf("second page = %d rows, cursor %q, want 1 row and no cursor", len(secondPage.Payments), secondPage.NextCursor)
	}

	seen := map[string]bool{}
	for _, payment := range append(firstPage.Payments, secondPage.Payments...) {
		if seen[payment.ID] {
			t.Fatalf("payment %s appeared on both pages", payment.ID)
		}
		seen[payment.ID] = true
	}

	badCursor := h.do(http.MethodGet, paymentsURL+"?cursor=not-a-cursor", nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	if badCursor.Status != http.StatusBadRequest {
		t.Fatalf("unusable cursor = %d, want 400", badCursor.Status)
	}
}

func TestCheckoutAndPaymentHistoryNeedAnAdminRole(t *testing.T) {
	h := newBillingHarness(t)
	owner := h.signIn("billing-owner@example.com")
	member := h.joinAs(owner, "billing-member@example.com", auth.RoleMember)
	admin := h.joinAs(owner, "billing-admin@example.com", auth.RoleAdmin)
	packID := h.packWithMinutes(owner.AccessToken, 100)

	memberCheckout := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": packID},
		withBearer(member.AccessToken), withWorkspace(member.Workspace.ID))
	if memberCheckout.Status != http.StatusForbidden {
		t.Fatalf("member checkout = %d, want 403", memberCheckout.Status)
	}

	memberHistory := h.do(http.MethodGet, paymentsURL, nil,
		withBearer(member.AccessToken), withWorkspace(member.Workspace.ID))
	if memberHistory.Status != http.StatusForbidden {
		t.Fatalf("member payment history = %d, want 403", memberHistory.Status)
	}

	memberBalance := h.do(http.MethodGet, balanceURL, nil,
		withBearer(member.AccessToken), withWorkspace(member.Workspace.ID))
	if memberBalance.Status != http.StatusOK {
		t.Fatalf("member balance = %d, want 200", memberBalance.Status)
	}

	memberPacks := h.do(http.MethodGet, packsURL, nil, withBearer(member.AccessToken))
	if memberPacks.Status != http.StatusOK {
		t.Fatalf("member packs = %d, want 200", memberPacks.Status)
	}

	adminCheckout := h.do(http.MethodPost, checkoutURL, map[string]string{"pack_id": packID},
		withBearer(admin.AccessToken), withWorkspace(admin.Workspace.ID))
	if adminCheckout.Status != http.StatusCreated {
		t.Fatalf("admin checkout: status %d, body %s", adminCheckout.Status, adminCheckout.Body)
	}

	adminHistory := h.do(http.MethodGet, paymentsURL, nil,
		withBearer(admin.AccessToken), withWorkspace(admin.Workspace.ID))
	if adminHistory.Status != http.StatusOK {
		t.Fatalf("admin payment history: status %d, body %s", adminHistory.Status, adminHistory.Body)
	}
}

func TestStalePendingPaymentsAreReaped(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("reaper@example.com")
	packID := h.packWithMinutes(session.AccessToken, 100)
	created := h.checkout(session, packID, "")

	reaped, err := h.service.ReapPendingPayments(h.context(), 24*time.Hour)
	if err != nil {
		t.Fatalf("reap a fresh payment: %v", err)
	}
	if reaped != 0 {
		t.Fatalf("a fresh pending payment was reaped: %d", reaped)
	}

	if _, err := h.pool.Exec(h.context(),
		`UPDATE payments SET created_at = now() - interval '48 hours' WHERE id = $1`, created.PaymentID); err != nil {
		t.Fatalf("age the payment: %v", err)
	}

	reaped, err = h.service.ReapPendingPayments(h.context(), 24*time.Hour)
	if err != nil {
		t.Fatalf("reap a stale payment: %v", err)
	}
	if reaped != 1 {
		t.Fatalf("reaped %d payments, want 1", reaped)
	}

	history := h.do(http.MethodGet, paymentsURL, nil,
		withBearer(session.AccessToken), withWorkspace(session.Workspace.ID))
	var listed paymentsPayload
	history.decode(t, &listed)
	if len(listed.Payments) != 1 || listed.Payments[0].Status != string(payments.StatusFailed) {
		t.Fatalf("payment history = %+v, want one failed row", listed.Payments)
	}

	repeat, err := h.service.ReapPendingPayments(h.context(), 24*time.Hour)
	if err != nil {
		t.Fatalf("second reap: %v", err)
	}
	if repeat != 0 {
		t.Fatalf("a failed payment was reaped again: %d", repeat)
	}

	if h.balance(uuid.MustParse(session.Workspace.ID)) != 0 {
		t.Fatal("reaping a payment moved the balance")
	}
}
