package payments

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
)

const (
	FakeProviderName    = "fake"
	FakeSecretHeader    = "X-Fake-Signature"
	fakeCheckoutBaseURL = "https://payments.invalid/fake"
)

type FakeOptions struct {
	Secret          string
	CheckoutBaseURL string
}

type Fake struct {
	secret      []byte
	checkoutURL string

	mu       sync.Mutex
	started  []StartPaymentRequest
	refunded []string
}

func NewFake(options FakeOptions) (*Fake, error) {
	secret := strings.TrimSpace(options.Secret)
	if secret == "" {
		return nil, fmt.Errorf("%w: the fake provider needs a shared secret", ErrNotConfigured)
	}
	checkout := strings.TrimRight(strings.TrimSpace(options.CheckoutBaseURL), "/")
	if checkout == "" {
		checkout = fakeCheckoutBaseURL
	}
	return &Fake{secret: []byte(secret), checkoutURL: checkout}, nil
}

func (f *Fake) StartPayment(_ context.Context, request StartPaymentRequest) (StartPaymentResponse, error) {
	if !request.Valid() {
		return StartPaymentResponse{}, fmt.Errorf("%w: reference and a positive amount in a three letter currency are required", ErrInvalidRequest)
	}

	f.mu.Lock()
	f.started = append(f.started, request)
	f.mu.Unlock()

	response := StartPaymentResponse{ProviderRef: strings.TrimSpace(request.Reference)}
	if strings.TrimSpace(request.Phone) != "" {
		response.PushSent = true
		return response, nil
	}
	response.RedirectURL = f.checkoutURL + "/" + response.ProviderRef
	return response, nil
}

type fakeWebhookBody struct {
	PaymentID   string `json:"payment_id"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"`
}

func (f *Fake) VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error) {
	if !constantTimeSecretMatch(f.secret, []byte(strings.TrimSpace(headers.Get(FakeSecretHeader)))) {
		return WebhookEvent{}, fmt.Errorf("%w: %s does not carry the shared secret", ErrInvalidSignature, FakeSecretHeader)
	}

	var parsed fakeWebhookBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return WebhookEvent{}, fmt.Errorf("%w: %v", ErrInvalidWebhook, err)
	}
	reference := strings.TrimSpace(parsed.PaymentID)
	if reference == "" {
		return WebhookEvent{}, fmt.Errorf("%w: payment_id is required", ErrInvalidWebhook)
	}

	status := StatusPaid
	if strings.TrimSpace(parsed.Status) != "" {
		parsedStatus, ok := ParseStatus(parsed.Status)
		if !ok {
			return WebhookEvent{}, fmt.Errorf("%w: status %q is not recognised", ErrInvalidWebhook, parsed.Status)
		}
		status = parsedStatus
	}

	return WebhookEvent{
		ProviderRef: reference,
		Reference:   reference,
		Status:      status,
		Amount:      Money{AmountMinor: parsed.AmountMinor, Currency: strings.ToUpper(strings.TrimSpace(parsed.Currency))},
		Raw:         json.RawMessage(append([]byte(nil), body...)),
	}, nil
}

func (f *Fake) Refund(_ context.Context, providerRef string, _ Money) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.refunded = append(f.refunded, providerRef)
	return nil
}

func (f *Fake) Started() []StartPaymentRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]StartPaymentRequest(nil), f.started...)
}

func (f *Fake) Refunds() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.refunded...)
}

func constantTimeSecretMatch(expected, provided []byte) bool {
	expectedDigest := sha256.Sum256(expected)
	providedDigest := sha256.Sum256(provided)
	return subtle.ConstantTimeCompare(expectedDigest[:], providedDigest[:]) == 1
}
