package payments

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type Status string

const (
	StatusPending  Status = "pending"
	StatusPaid     Status = "paid"
	StatusFailed   Status = "failed"
	StatusRefunded Status = "refunded"
)

var (
	ErrNotConfigured      = errors.New("payments: provider is not configured")
	ErrInvalidRequest     = errors.New("payments: payment request is not valid")
	ErrInvalidSignature   = errors.New("payments: webhook signature is not valid")
	ErrInvalidWebhook     = errors.New("payments: webhook body is not usable")
	ErrProviderFailed     = errors.New("payments: the provider could not be reached")
	ErrProviderRejected   = errors.New("payments: provider refused the request")
	ErrProviderUnknown    = errors.New("payments: provider is not known")
	ErrProviderRegistered = errors.New("payments: provider is already registered")
	ErrRefundUnsupported  = errors.New("payments: provider does not support refunds yet")
)

func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusPaid, StatusFailed, StatusRefunded:
		return true
	default:
		return false
	}
}

func ParseStatus(value string) (Status, bool) {
	candidate := Status(strings.ToLower(strings.TrimSpace(value)))
	if !candidate.Valid() {
		return "", false
	}
	return candidate, true
}

type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

var currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)

func (m Money) Valid() bool {
	return m.AmountMinor > 0 && currencyPattern.MatchString(m.Currency)
}

type StartPaymentRequest struct {
	WorkspaceID uuid.UUID
	Amount      Money
	Phone       string
	Reference   string
	CallbackURL string
	Description string
}

func (r StartPaymentRequest) Valid() bool {
	return r.Amount.Valid() && strings.TrimSpace(r.Reference) != ""
}

type StartPaymentResponse struct {
	ProviderRef string
	RedirectURL string
	PushSent    bool
}

type WebhookEvent struct {
	ProviderRef string
	Reference   string
	Status      Status
	Amount      Money
	Raw         json.RawMessage
}

type PaymentProvider interface {
	StartPayment(ctx context.Context, req StartPaymentRequest) (StartPaymentResponse, error)
	VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error)
	Refund(ctx context.Context, providerRef string, amount Money) error
}
