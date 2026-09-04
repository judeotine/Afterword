package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/payments"
)

const (
	DefaultPageSize       int32 = 25
	MaxPageSize           int32 = 100
	MaxPhoneLength              = 32
	DefaultPendingTTL           = 24 * time.Hour
	DefaultCheckoutLimit        = int64(10)
	DefaultCheckoutWindow       = time.Hour

	actionPaymentReview  = "billing.payment.needs_review"
	actionCreditsAdjust  = "billing.credits.adjust"
	adminActor           = "admin-token"
	reasonAmountMismatch = "the provider reported a different amount"
	reasonLateSettlement = "a paid webhook arrived for a payment that was no longer pending"
	webhookPathPrefix    = "/v1/billing/webhooks/"
)

var (
	ErrPackNotFound      = errors.New("billing: credit pack not found")
	ErrPaymentNotFound   = errors.New("billing: payment not found")
	ErrProviderNotUsable = errors.New("billing: no payment provider is available")
	ErrInvalidPhone      = errors.New("billing: phone number is not usable")
	ErrInvalidCursor     = errors.New("billing: cursor is not valid")
	ErrCheckoutLimited   = errors.New("billing: too many checkouts were started for this workspace")
)

type ServiceOptions struct {
	Pool             *pgxpool.Pool
	Ledger           *credits.Ledger
	Providers        *payments.Registry
	DefaultProvider  string
	FreeGrantMinutes int32
	APIBaseURL       string
	CheckoutLimit    int64
	CheckoutWindow   time.Duration
	Logger           zerolog.Logger
	Clock            func() time.Time
}

type Service struct {
	pool             *pgxpool.Pool
	queries          *sqlcgen.Queries
	ledger           *credits.Ledger
	providers        *payments.Registry
	defaultProvider  string
	freeGrantMinutes int32
	apiBaseURL       string
	checkoutLimit    int64
	checkoutWindow   time.Duration
	logger           zerolog.Logger
	clock            func() time.Time
}

func NewService(options ServiceOptions) (*Service, error) {
	switch {
	case options.Pool == nil:
		return nil, errors.New("billing: a database pool is required")
	case options.Providers == nil:
		return nil, errors.New("billing: a payment provider registry is required")
	}

	ledger := options.Ledger
	if ledger == nil {
		ledger = credits.NewLedger(options.Pool)
	}
	clock := options.Clock
	if clock == nil {
		clock = time.Now
	}
	freeGrant := options.FreeGrantMinutes
	if freeGrant < 0 {
		freeGrant = 0
	}

	checkoutLimit := options.CheckoutLimit
	if checkoutLimit <= 0 {
		checkoutLimit = DefaultCheckoutLimit
	}
	checkoutWindow := options.CheckoutWindow
	if checkoutWindow <= 0 {
		checkoutWindow = DefaultCheckoutWindow
	}

	service := &Service{
		pool:             options.Pool,
		queries:          sqlcgen.New(options.Pool),
		ledger:           ledger,
		providers:        options.Providers,
		defaultProvider:  strings.ToLower(strings.TrimSpace(options.DefaultProvider)),
		freeGrantMinutes: freeGrant,
		apiBaseURL:       strings.TrimRight(strings.TrimSpace(options.APIBaseURL), "/"),
		checkoutLimit:    checkoutLimit,
		checkoutWindow:   checkoutWindow,
		logger:           options.Logger,
		clock:            clock,
	}
	if service.defaultProvider != "" {
		if err := options.Providers.SetDefault(service.defaultProvider); err != nil {
			return nil, err
		}
	}
	return service, nil
}

type Pack struct {
	ID         uuid.UUID
	Name       string
	Minutes    int32
	PriceMinor int64
	Currency   string
}

type Balance struct {
	Minutes          int64
	GrantExpiresAt   *time.Time
	FreeGrantMinutes int32
}

type Payment struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	PackID      *uuid.UUID
	Provider    string
	ProviderRef string
	AmountMinor int64
	Currency    string
	Minutes     int32
	Status      string
	PaidAt      *time.Time
	CreatedAt   time.Time
}

type Checkout struct {
	Payment     Payment
	RedirectURL string
	PushSent    bool
}

type WebhookResult struct {
	Payment     Payment
	Credited    bool
	Duplicate   bool
	NeedsReview bool
}

func (s *Service) FreeGrantMinutes() int32 {
	return s.freeGrantMinutes
}

func (s *Service) ListPacks(ctx context.Context) ([]Pack, error) {
	rows, err := s.queries.ListActiveCreditPacks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list credit packs: %w", err)
	}
	packs := make([]Pack, 0, len(rows))
	for _, row := range rows {
		packs = append(packs, newPack(row))
	}
	return packs, nil
}

func (s *Service) Balance(ctx context.Context, workspaceID uuid.UUID) (Balance, error) {
	minutes, err := s.ledger.Balance(ctx, workspaceID)
	if err != nil {
		return Balance{}, err
	}
	balance := Balance{Minutes: minutes, FreeGrantMinutes: s.freeGrantMinutes}

	grant, err := s.queries.GetActiveCreditGrant(ctx, workspaceID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return balance, nil
	case err != nil:
		return Balance{}, fmt.Errorf("read the current credit grant: %w", err)
	}
	if grant.ExpiresAt.Valid {
		expiresAt := grant.ExpiresAt.Time.UTC()
		balance.GrantExpiresAt = &expiresAt
	}
	return balance, nil
}

func (s *Service) Adjust(ctx context.Context, workspaceID uuid.UUID, deltaMinutes int32, refID string) (Balance, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Balance{}, fmt.Errorf("begin credit adjustment: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	entry, err := s.ledger.AdjustTx(ctx, tx, workspaceID, deltaMinutes, refID)
	if err != nil {
		return Balance{}, err
	}
	if err := s.audit(ctx, tx, workspaceID, actionCreditsAdjust, adjustTarget(entry.ID, deltaMinutes, refID)); err != nil {
		return Balance{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Balance{}, fmt.Errorf("commit credit adjustment: %w", err)
	}
	return s.Balance(ctx, workspaceID)
}

func adjustTarget(entryID uuid.UUID, deltaMinutes int32, refID string) string {
	target := adminActor + " " + strconv.FormatInt(int64(deltaMinutes), 10) + " minutes entry:" + entryID.String()
	if strings.TrimSpace(refID) != "" {
		target += " ref:" + refID
	}
	return target
}

func (s *Service) audit(ctx context.Context, tx pgx.Tx, workspaceID uuid.UUID, action, target string) error {
	if _, err := s.queries.WithTx(tx).CreateAuditLogEntry(ctx, sqlcgen.CreateAuditLogEntryParams{
		WorkspaceID: workspaceID,
		Action:      action,
		Target:      target,
		At:          pgtype.Timestamptz{Time: s.clock().UTC(), Valid: true},
	}); err != nil {
		return fmt.Errorf("write the audit entry: %w", err)
	}
	return nil
}

func (s *Service) Checkout(ctx context.Context, workspaceID, packID uuid.UUID, phone string) (Checkout, error) {
	phone, err := normalizePhone(phone)
	if err != nil {
		return Checkout{}, err
	}

	pack, err := s.queries.GetActiveCreditPack(ctx, packID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Checkout{}, ErrPackNotFound
		}
		return Checkout{}, fmt.Errorf("read credit pack: %w", err)
	}

	providerName, provider, err := s.provider("")
	if err != nil {
		return Checkout{}, err
	}

	paymentID := uuid.New()
	reference := paymentID.String()
	row, err := s.reservePayment(ctx, sqlcgen.InsertPaymentParams{
		ID:          paymentID,
		WorkspaceID: workspaceID,
		PackID:      &pack.ID,
		Provider:    providerName,
		ProviderRef: reference,
		AmountMinor: pack.PriceMinor,
		Currency:    pack.Currency,
		Minutes:     pack.Minutes,
		Raw:         []byte(`{}`),
	})
	if err != nil {
		return Checkout{}, err
	}

	started, err := provider.StartPayment(ctx, payments.StartPaymentRequest{
		WorkspaceID: workspaceID,
		Amount:      payments.Money{AmountMinor: pack.PriceMinor, Currency: pack.Currency},
		Phone:       phone,
		Reference:   reference,
		CallbackURL: s.callbackURL(providerName),
		Description: pack.Name,
	})
	if err != nil {
		s.abandonPayment(ctx, paymentID, err)
		return Checkout{}, err
	}

	if started.ProviderRef != "" && started.ProviderRef != reference {
		updated, refErr := s.queries.SetPaymentProviderRef(ctx, sqlcgen.SetPaymentProviderRefParams{
			ProviderRef: started.ProviderRef,
			ID:          paymentID,
		})
		if refErr != nil {
			return Checkout{}, fmt.Errorf("record the provider reference: %w", refErr)
		}
		row = updated
	}

	return Checkout{
		Payment:     newPayment(row),
		RedirectURL: started.RedirectURL,
		PushSent:    started.PushSent,
	}, nil
}

func (s *Service) HandleWebhook(ctx context.Context, providerName string, headers http.Header, body []byte) (WebhookResult, error) {
	name, provider, err := s.provider(providerName)
	if err != nil {
		return WebhookResult{}, err
	}

	event, err := provider.VerifyWebhook(headers, body)
	if err != nil {
		return WebhookResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return WebhookResult{}, fmt.Errorf("begin webhook transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := s.queries.WithTx(tx)
	row, err := queries.LockPaymentByProviderRef(ctx, sqlcgen.LockPaymentByProviderRefParams{
		Provider:    name,
		ProviderRef: event.ProviderRef,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WebhookResult{}, ErrPaymentNotFound
		}
		return WebhookResult{}, fmt.Errorf("lock the payment: %w", err)
	}

	if row.Status != string(payments.StatusPending) {
		if event.Status == payments.StatusPaid && row.Status != string(payments.StatusPaid) {
			return s.flagForReview(ctx, tx, row, reasonLateSettlement)
		}
		return WebhookResult{Payment: newPayment(row), Duplicate: true}, nil
	}

	raw := event.Raw
	if len(raw) == 0 || !json.Valid(raw) {
		raw = json.RawMessage(`{}`)
	}

	status := event.Status
	if status == payments.StatusPending {
		return WebhookResult{Payment: newPayment(row)}, nil
	}

	if status == payments.StatusPaid && !amountMatches(event.Amount, row) {
		return s.flagForReview(ctx, tx, row, reasonAmountMismatch)
	}

	settled := sqlcgen.SettlePaymentParams{Status: string(status), Raw: raw, ID: row.ID}
	credited := false
	if status == payments.StatusPaid {
		now := s.clock().UTC()
		settled.PaidAt = pgtype.Timestamptz{Time: now, Valid: true}
		if _, err := s.ledger.PurchaseTx(ctx, tx, row.WorkspaceID, row.Minutes, "payment:"+row.ID.String()); err != nil {
			return WebhookResult{}, fmt.Errorf("credit the purchase: %w", err)
		}
		credited = true
	}

	updated, err := queries.SettlePayment(ctx, settled)
	if err != nil {
		return WebhookResult{}, fmt.Errorf("settle the payment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return WebhookResult{}, fmt.Errorf("commit the webhook: %w", err)
	}

	return WebhookResult{Payment: newPayment(updated), Credited: credited}, nil
}

func amountMatches(amount payments.Money, row sqlcgen.Payment) bool {
	if amount.AmountMinor == 0 && strings.TrimSpace(amount.Currency) == "" {
		return true
	}
	if amount.AmountMinor != row.AmountMinor {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(amount.Currency), row.Currency)
}

func (s *Service) flagForReview(ctx context.Context, tx pgx.Tx, row sqlcgen.Payment, reason string) (WebhookResult, error) {
	s.logger.Error().
		Str("payment_id", row.ID.String()).
		Str("provider", row.Provider).
		Str("provider_ref", row.ProviderRef).
		Str("status", row.Status).
		Str("reason", reason).
		Msg("a payment webhook could not be settled and needs review")

	queries := s.queries.WithTx(tx)
	raw, err := json.Marshal(map[string]string{"afterword_review": reason})
	if err != nil {
		raw = []byte(`{}`)
	}
	updated, err := queries.MarkPaymentNeedsReview(ctx, sqlcgen.MarkPaymentNeedsReviewParams{Reason: raw, ID: row.ID})
	if err != nil {
		return WebhookResult{}, fmt.Errorf("flag the payment for review: %w", err)
	}
	if err := s.audit(ctx, tx, row.WorkspaceID, actionPaymentReview, reviewTarget(row, reason)); err != nil {
		return WebhookResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return WebhookResult{}, fmt.Errorf("commit the payment review: %w", err)
	}
	return WebhookResult{Payment: newPayment(updated), NeedsReview: true}, nil
}

func reviewTarget(row sqlcgen.Payment, reason string) string {
	return "payment:" + row.ID.String() + " " + row.Provider + ":" + row.ProviderRef + " " + reason
}

func (s *Service) ListPayments(ctx context.Context, workspaceID uuid.UUID, cursor string, pageSize int32) ([]Payment, string, error) {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	params := sqlcgen.ListPaymentsByWorkspaceParams{WorkspaceID: workspaceID, PageSize: pageSize + 1}
	if strings.TrimSpace(cursor) != "" {
		at, id, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		params.CursorCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.CursorID = &id
	}

	rows, err := s.queries.ListPaymentsByWorkspace(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("list payments: %w", err)
	}

	next := ""
	if int32(len(rows)) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = EncodeCursor(last.CreatedAt.Time, last.ID)
	}

	result := make([]Payment, 0, len(rows))
	for _, row := range rows {
		result = append(result, newPayment(row))
	}
	return result, next, nil
}

func (s *Service) ReapPendingPayments(ctx context.Context, olderThan time.Duration) (int, error) {
	if olderThan <= 0 {
		olderThan = DefaultPendingTTL
	}
	cutoff := s.clock().UTC().Add(-olderThan)

	rows, err := s.queries.FailStalePendingPayments(ctx, sqlcgen.FailStalePendingPaymentsParams{
		Reason:    []byte(`{"afterword_reason":"expired before the provider confirmed it"}`),
		OlderThan: pgtype.Timestamptz{Time: cutoff, Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("reap stale pending payments: %w", err)
	}
	for _, row := range rows {
		s.logger.Warn().
			Str("payment_id", row.ID.String()).
			Str("provider", row.Provider).
			Str("provider_ref", row.ProviderRef).
			Msg("a pending payment expired without a provider webhook")
	}
	return len(rows), nil
}

func (s *Service) provider(name string) (string, payments.PaymentProvider, error) {
	trimmed := strings.ToLower(strings.TrimSpace(name))
	if trimmed == "" {
		defaultName, provider, err := s.providers.Default()
		if err != nil {
			return "", nil, fmt.Errorf("%w: %v", ErrProviderNotUsable, err)
		}
		return defaultName, provider, nil
	}
	provider, err := s.providers.Lookup(trimmed)
	if err != nil {
		return "", nil, err
	}
	return trimmed, provider, nil
}

func (s *Service) callbackURL(providerName string) string {
	if s.apiBaseURL == "" {
		return ""
	}
	return s.apiBaseURL + webhookPathPrefix + providerName
}

func (s *Service) abandonPayment(ctx context.Context, paymentID uuid.UUID, cause error) {
	if !errors.Is(cause, payments.ErrProviderRejected) && !errors.Is(cause, payments.ErrInvalidRequest) {
		s.logger.Warn().
			Err(cause).
			Str("payment_id", paymentID.String()).
			Msg("a checkout could not reach the provider: the payment stays pending for the reaper")
		return
	}

	failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	raw, err := json.Marshal(map[string]string{"afterword_error": cause.Error()})
	if err != nil {
		raw = []byte(`{}`)
	}
	if _, err := s.queries.FailPendingPayment(failCtx, sqlcgen.FailPendingPaymentParams{Raw: raw, ID: paymentID}); err != nil {
		s.logger.Error().Err(err).Str("payment_id", paymentID.String()).Msg("marking an abandoned payment failed")
	}
}

func (s *Service) reservePayment(ctx context.Context, params sqlcgen.InsertPaymentParams) (sqlcgen.Payment, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return sqlcgen.Payment{}, fmt.Errorf("begin checkout: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	queries := s.queries.WithTx(tx)
	if err := queries.LockWorkspaceCheckouts(ctx, params.WorkspaceID.String()); err != nil {
		return sqlcgen.Payment{}, fmt.Errorf("lock the workspace checkouts: %w", err)
	}

	since := s.clock().UTC().Add(-s.checkoutWindow)
	started, err := queries.CountRecentCheckouts(ctx, sqlcgen.CountRecentCheckoutsParams{
		WorkspaceID: params.WorkspaceID,
		Since:       pgtype.Timestamptz{Time: since, Valid: true},
	})
	if err != nil {
		return sqlcgen.Payment{}, fmt.Errorf("count recent checkouts: %w", err)
	}
	if started >= s.checkoutLimit {
		return sqlcgen.Payment{}, fmt.Errorf("%w: %d in the last %s", ErrCheckoutLimited, started, s.checkoutWindow)
	}

	row, err := queries.InsertPayment(ctx, params)
	if err != nil {
		return sqlcgen.Payment{}, fmt.Errorf("create pending payment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlcgen.Payment{}, fmt.Errorf("commit the checkout: %w", err)
	}
	return row, nil
}

var phoneSeparators = strings.NewReplacer(" ", "", "-", "", "(", "", ")", "", ".", "", "\u00a0", "")

func normalizePhone(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", nil
	}
	if len([]rune(trimmed)) > MaxPhoneLength {
		return "", ErrInvalidPhone
	}
	normalized, err := auth.NormalizePhone(phoneSeparators.Replace(trimmed))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPhone, err)
	}
	return normalized, nil
}

func newPack(row sqlcgen.CreditPack) Pack {
	return Pack{
		ID:         row.ID,
		Name:       row.Name,
		Minutes:    row.Minutes,
		PriceMinor: row.PriceMinor,
		Currency:   row.Currency,
	}
}

func newPayment(row sqlcgen.Payment) Payment {
	payment := Payment{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		PackID:      row.PackID,
		Provider:    row.Provider,
		ProviderRef: row.ProviderRef,
		AmountMinor: row.AmountMinor,
		Currency:    row.Currency,
		Minutes:     row.Minutes,
		Status:      row.Status,
	}
	if row.CreatedAt.Valid {
		payment.CreatedAt = row.CreatedAt.Time.UTC()
	}
	if row.PaidAt.Valid {
		paidAt := row.PaidAt.Time.UTC()
		payment.PaidAt = &paidAt
	}
	return payment
}
