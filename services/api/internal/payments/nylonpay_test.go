package payments

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

const (
	knownWebhookSecret = "whsec_afterword_test_secret"
	knownWebhookBody   = `{"provider_ref":"np_01HZY9","reference":"1f2b3c4d-0000-4000-8000-000000000001","status":"paid","amount":60000,"currency":"UGX"}`
	knownWebhookDigest = "94db30fafc6fb23a3b1e25fc22b93c32b05f3b11d58a24056961d48e6a6488bf"
)

func newTestNylonPay(t *testing.T) *NylonPay {
	t.Helper()
	provider, err := NewNylonPay(NylonPayOptions{
		BaseURL:       "https://pay.example.test/api",
		APIKey:        "key_live_test",
		WebhookSecret: knownWebhookSecret,
	})
	if err != nil {
		t.Fatalf("new nylonpay: %v", err)
	}
	return provider
}

func TestNylonPaySignatureMatchesTheKnownVector(t *testing.T) {
	if got := SignNylonPayBody(knownWebhookSecret, []byte(knownWebhookBody)); got != knownWebhookDigest {
		t.Fatalf("SignNylonPayBody = %s, want %s", got, knownWebhookDigest)
	}
}

func TestNylonPayVerifyWebhookAcceptsTheKnownVector(t *testing.T) {
	provider := newTestNylonPay(t)
	headers := http.Header{}
	headers.Set(NylonPaySignatureHeader, knownWebhookDigest)

	event, err := provider.VerifyWebhook(headers, []byte(knownWebhookBody))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if event.ProviderRef != "np_01HZY9" {
		t.Errorf("ProviderRef = %q, want np_01HZY9", event.ProviderRef)
	}
	if event.Reference != "1f2b3c4d-0000-4000-8000-000000000001" {
		t.Errorf("Reference = %q", event.Reference)
	}
	if event.Status != StatusPaid {
		t.Errorf("Status = %q, want paid", event.Status)
	}
	if event.Amount.AmountMinor != 60000 || event.Amount.Currency != "UGX" {
		t.Errorf("Amount = %+v, want 60000 UGX", event.Amount)
	}
	if !json.Valid(event.Raw) {
		t.Errorf("Raw is not valid json: %s", event.Raw)
	}
}

func TestNylonPayVerifyWebhookAcceptsAPrefixedSignature(t *testing.T) {
	provider := newTestNylonPay(t)
	headers := http.Header{}
	headers.Set(NylonPaySignatureHeader, "sha256="+strings.ToUpper(knownWebhookDigest))

	if _, err := provider.VerifyWebhook(headers, []byte(knownWebhookBody)); err != nil {
		t.Fatalf("VerifyWebhook with prefixed signature: %v", err)
	}
}

func TestNylonPayVerifyWebhookRejectsTamperedBodiesAndSignatures(t *testing.T) {
	provider := newTestNylonPay(t)

	cases := []struct {
		name      string
		signature string
		body      string
	}{
		{"missing signature", "", knownWebhookBody},
		{"wrong signature", strings.Repeat("a", 64), knownWebhookBody},
		{"truncated signature", knownWebhookDigest[:32], knownWebhookBody},
		{"not hex", strings.Repeat("z", 64), knownWebhookBody},
		{"tampered body", knownWebhookDigest, strings.Replace(knownWebhookBody, "60000", "60001", 1)},
		{"whitespace added to the body", knownWebhookDigest, knownWebhookBody + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.signature != "" {
				headers.Set(NylonPaySignatureHeader, tc.signature)
			}
			if _, err := provider.VerifyWebhook(headers, []byte(tc.body)); !errors.Is(err, ErrInvalidSignature) {
				t.Fatalf("VerifyWebhook error = %v, want ErrInvalidSignature", err)
			}
		})
	}
}

func TestNylonPayVerifyWebhookRejectsAValidlySignedButUnusableBody(t *testing.T) {
	provider := newTestNylonPay(t)
	body := []byte(`{"status":"paid"}`)
	headers := http.Header{}
	headers.Set(NylonPaySignatureHeader, SignNylonPayBody(knownWebhookSecret, body))

	if _, err := provider.VerifyWebhook(headers, body); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("VerifyWebhook error = %v, want ErrInvalidWebhook", err)
	}
}

func TestNewNylonPayRequiresItsConfiguration(t *testing.T) {
	cases := []NylonPayOptions{
		{APIKey: "k", WebhookSecret: "s"},
		{BaseURL: "https://pay.example.test", WebhookSecret: "s"},
		{BaseURL: "https://pay.example.test", APIKey: "k"},
		{BaseURL: "not-a-url", APIKey: "k", WebhookSecret: "s"},
	}
	for _, options := range cases {
		if _, err := NewNylonPay(options); !errors.Is(err, ErrNotConfigured) {
			t.Fatalf("NewNylonPay(%+v) error = %v, want ErrNotConfigured", options, err)
		}
	}
}

func TestNylonPayRefundIsNotImplementedYet(t *testing.T) {
	provider := newTestNylonPay(t)
	err := provider.Refund(t.Context(), "np_01HZY9", Money{AmountMinor: 60000, Currency: "UGX"})
	if !errors.Is(err, ErrRefundUnsupported) {
		t.Fatalf("Refund error = %v, want ErrRefundUnsupported", err)
	}
}

func TestNylonPayStartPaymentSendsTheDocumentedBody(t *testing.T) {
	var (
		gotPath   string
		gotAuth   string
		gotKey    string
		gotBody   map[string]any
		gotMethod string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotKey = r.Header.Get("Idempotency-Key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"provider_ref":"np_777","redirect_url":"https://pay.example.test/np_777","push_sent":true}`))
	}))
	defer server.Close()

	provider, err := NewNylonPay(NylonPayOptions{
		BaseURL:       server.URL + "/api",
		APIKey:        "key_live_test",
		WebhookSecret: knownWebhookSecret,
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatalf("new nylonpay: %v", err)
	}

	response, err := provider.StartPayment(t.Context(), StartPaymentRequest{
		Amount:      Money{AmountMinor: 60000, Currency: "UGX"},
		Phone:       "+256700000000",
		Reference:   "ref-1",
		CallbackURL: "https://api.example.test/v1/billing/webhooks/nylonpay",
	})
	if err != nil {
		t.Fatalf("StartPayment: %v", err)
	}
	if response.ProviderRef != "np_777" || response.RedirectURL != "https://pay.example.test/np_777" || !response.PushSent {
		t.Fatalf("StartPayment response = %+v", response)
	}
	if gotMethod != http.MethodPost || gotPath != "/api/payments" {
		t.Errorf("request = %s %s, want POST /api/payments", gotMethod, gotPath)
	}
	if gotAuth != "Bearer key_live_test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotKey != "ref-1" {
		t.Errorf("Idempotency-Key = %q, want ref-1", gotKey)
	}
	want := map[string]any{
		"amount":       float64(60000),
		"currency":     "UGX",
		"phone":        "+256700000000",
		"reference":    "ref-1",
		"callback_url": "https://api.example.test/v1/billing/webhooks/nylonpay",
	}
	if !reflect.DeepEqual(gotBody, want) {
		t.Errorf("request body = %#v, want %#v", gotBody, want)
	}
}

func TestNylonPayStartPaymentReportsProviderFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream down"}`))
	}))
	defer server.Close()

	provider, err := NewNylonPay(NylonPayOptions{
		BaseURL:       server.URL,
		APIKey:        "key",
		WebhookSecret: knownWebhookSecret,
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatalf("new nylonpay: %v", err)
	}

	_, err = provider.StartPayment(t.Context(), StartPaymentRequest{
		Amount:    Money{AmountMinor: 100, Currency: "UGX"},
		Reference: "ref-2",
	})
	if !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("StartPayment error = %v, want ErrProviderFailed", err)
	}
}

func TestNylonPayStartPaymentRequiresAProviderRef(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"redirect_url":"https://pay.example.test/x"}`))
	}))
	defer server.Close()

	provider, err := NewNylonPay(NylonPayOptions{
		BaseURL:       server.URL,
		APIKey:        "key",
		WebhookSecret: knownWebhookSecret,
		HTTPClient:    server.Client(),
	})
	if err != nil {
		t.Fatalf("new nylonpay: %v", err)
	}

	if _, err := provider.StartPayment(t.Context(), StartPaymentRequest{
		Amount:    Money{AmountMinor: 100, Currency: "UGX"},
		Reference: "ref-3",
	}); !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("StartPayment error = %v, want ErrProviderFailed", err)
	}
}
