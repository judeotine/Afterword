package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	NylonPayProviderName    = "nylonpay"
	NylonPaySignatureHeader = "X-Nylon-Signature"
	NylonPayPaymentsPath    = "/payments"

	nylonPaySignaturePrefix = "sha256="
	nylonPayDefaultTimeout  = 20 * time.Second
	nylonPayMaxResponse     = 1 << 20
)

type NylonPayOptions struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
	HTTPClient    *http.Client
	Timeout       time.Duration
}

type NylonPay struct {
	baseURL       string
	apiKey        string
	webhookSecret []byte
	client        *http.Client
}

func NewNylonPay(options NylonPayOptions) (*NylonPay, error) {
	base := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	apiKey := strings.TrimSpace(options.APIKey)
	secret := strings.TrimSpace(options.WebhookSecret)

	switch {
	case base == "":
		return nil, fmt.Errorf("%w: NYLONPAY_BASE_URL is required", ErrNotConfigured)
	case apiKey == "":
		return nil, fmt.Errorf("%w: NYLONPAY_API_KEY is required", ErrNotConfigured)
	case secret == "":
		return nil, fmt.Errorf("%w: NYLONPAY_WEBHOOK_SECRET is required", ErrNotConfigured)
	}

	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("%w: NYLONPAY_BASE_URL must be an absolute http or https url", ErrNotConfigured)
	}

	client := options.HTTPClient
	if client == nil {
		timeout := options.Timeout
		if timeout <= 0 {
			timeout = nylonPayDefaultTimeout
		}
		client = &http.Client{Timeout: timeout}
	}

	return &NylonPay{baseURL: base, apiKey: apiKey, webhookSecret: []byte(secret), client: client}, nil
}

type nylonPayStartRequest struct {
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
	Phone       string `json:"phone,omitempty"`
	Reference   string `json:"reference"`
	CallbackURL string `json:"callback_url"`
}

type nylonPayStartResponse struct {
	ProviderRef string `json:"provider_ref"`
	RedirectURL string `json:"redirect_url"`
	PushSent    bool   `json:"push_sent"`
}

func (n *NylonPay) StartPayment(ctx context.Context, request StartPaymentRequest) (StartPaymentResponse, error) {
	if !request.Valid() {
		return StartPaymentResponse{}, fmt.Errorf("%w: reference and a positive amount in a three letter currency are required", ErrInvalidRequest)
	}

	payload, err := json.Marshal(nylonPayStartRequest{
		Amount:      request.Amount.AmountMinor,
		Currency:    request.Amount.Currency,
		Phone:       strings.TrimSpace(request.Phone),
		Reference:   strings.TrimSpace(request.Reference),
		CallbackURL: strings.TrimSpace(request.CallbackURL),
	})
	if err != nil {
		return StartPaymentResponse{}, fmt.Errorf("encode nylonpay request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+NylonPayPaymentsPath, bytes.NewReader(payload))
	if err != nil {
		return StartPaymentResponse{}, fmt.Errorf("build nylonpay request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+n.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("Idempotency-Key", strings.TrimSpace(request.Reference))

	response, err := n.client.Do(httpRequest)
	if err != nil {
		return StartPaymentResponse{}, fmt.Errorf("%w: %v", ErrProviderFailed, err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(response.Body, nylonPayMaxResponse))
	if err != nil {
		return StartPaymentResponse{}, fmt.Errorf("%w: reading the response failed: %v", ErrProviderFailed, err)
	}
	if response.StatusCode >= http.StatusBadRequest && response.StatusCode < http.StatusInternalServerError {
		return StartPaymentResponse{}, fmt.Errorf("%w: status %d: %s", ErrProviderRejected, response.StatusCode, truncate(body))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return StartPaymentResponse{}, fmt.Errorf("%w: status %d: %s", ErrProviderFailed, response.StatusCode, truncate(body))
	}

	var parsed nylonPayStartResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return StartPaymentResponse{}, fmt.Errorf("%w: response is not json: %v", ErrProviderFailed, err)
	}
	providerRef := strings.TrimSpace(parsed.ProviderRef)
	if providerRef == "" {
		return StartPaymentResponse{}, fmt.Errorf("%w: the response carried no provider_ref", ErrProviderFailed)
	}

	return StartPaymentResponse{
		ProviderRef: providerRef,
		RedirectURL: strings.TrimSpace(parsed.RedirectURL),
		PushSent:    parsed.PushSent,
	}, nil
}

type nylonPayWebhookBody struct {
	ProviderRef string `json:"provider_ref"`
	Reference   string `json:"reference"`
	Status      string `json:"status"`
	Amount      int64  `json:"amount"`
	Currency    string `json:"currency"`
}

func (n *NylonPay) VerifyWebhook(headers http.Header, body []byte) (WebhookEvent, error) {
	provided := strings.TrimSpace(headers.Get(NylonPaySignatureHeader))
	provided = strings.TrimPrefix(provided, nylonPaySignaturePrefix)
	if provided == "" {
		return WebhookEvent{}, fmt.Errorf("%w: %s is missing", ErrInvalidSignature, NylonPaySignatureHeader)
	}

	decoded, err := hex.DecodeString(strings.ToLower(provided))
	if err != nil {
		return WebhookEvent{}, fmt.Errorf("%w: %s is not hex", ErrInvalidSignature, NylonPaySignatureHeader)
	}
	mac := hmac.New(sha256.New, n.webhookSecret)
	mac.Write(body)
	if !hmac.Equal(decoded, mac.Sum(nil)) {
		return WebhookEvent{}, fmt.Errorf("%w: %s does not match the body", ErrInvalidSignature, NylonPaySignatureHeader)
	}

	var parsed nylonPayWebhookBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		return WebhookEvent{}, fmt.Errorf("%w: %v", ErrInvalidWebhook, err)
	}
	providerRef := strings.TrimSpace(parsed.ProviderRef)
	if providerRef == "" {
		return WebhookEvent{}, fmt.Errorf("%w: provider_ref is required", ErrInvalidWebhook)
	}
	status, ok := ParseStatus(parsed.Status)
	if !ok {
		return WebhookEvent{}, fmt.Errorf("%w: status %q is not recognised", ErrInvalidWebhook, parsed.Status)
	}

	return WebhookEvent{
		ProviderRef: providerRef,
		Reference:   strings.TrimSpace(parsed.Reference),
		Status:      status,
		Amount:      Money{AmountMinor: parsed.Amount, Currency: strings.ToUpper(strings.TrimSpace(parsed.Currency))},
		Raw:         json.RawMessage(append([]byte(nil), body...)),
	}, nil
}

func (n *NylonPay) Refund(_ context.Context, _ string, _ Money) error {
	return fmt.Errorf("%w: the Nylon Pay refund api is not documented yet", ErrRefundUnsupported)
}

func SignNylonPayBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func truncate(body []byte) string {
	const limit = 256
	if len(body) <= limit {
		return string(body)
	}
	return string(body[:limit]) + "..."
}
