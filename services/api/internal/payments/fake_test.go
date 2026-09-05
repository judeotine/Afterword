package payments

import (
	"errors"
	"net/http"
	"testing"
)

func newTestFake(t *testing.T) *Fake {
	t.Helper()
	provider, err := NewFake(FakeOptions{Secret: "fake-secret-value"})
	if err != nil {
		t.Fatalf("new fake: %v", err)
	}
	return provider
}

func TestFakeStartPaymentEchoesTheReferenceAsTheProviderRef(t *testing.T) {
	provider := newTestFake(t)

	pushed, err := provider.StartPayment(t.Context(), StartPaymentRequest{
		Amount:      Money{AmountMinor: 15000, Currency: "UGX"},
		Phone:       "+256700000000",
		Reference:   "ref-push",
		CallbackURL: "http://localhost:8080/v1/billing/webhooks/fake",
	})
	if err != nil {
		t.Fatalf("StartPayment: %v", err)
	}
	if pushed.ProviderRef != "ref-push" {
		t.Errorf("ProviderRef = %q, want ref-push", pushed.ProviderRef)
	}
	if !pushed.PushSent {
		t.Error("PushSent = false, want true when a phone number is given")
	}
	if pushed.RedirectURL != "" {
		t.Errorf("RedirectURL = %q, want empty for a push payment", pushed.RedirectURL)
	}

	redirected, err := provider.StartPayment(t.Context(), StartPaymentRequest{
		Amount:      Money{AmountMinor: 15000, Currency: "UGX"},
		Reference:   "ref-redirect",
		CallbackURL: "http://localhost:8080/v1/billing/webhooks/fake",
	})
	if err != nil {
		t.Fatalf("StartPayment without a phone: %v", err)
	}
	if redirected.PushSent {
		t.Error("PushSent = true, want false without a phone number")
	}
	if redirected.RedirectURL == "" {
		t.Error("RedirectURL is empty, want a checkout url without a phone number")
	}
}

func TestFakeStartPaymentRejectsABadRequest(t *testing.T) {
	provider := newTestFake(t)
	cases := []StartPaymentRequest{
		{Amount: Money{AmountMinor: 100, Currency: "UGX"}},
		{Amount: Money{AmountMinor: 0, Currency: "UGX"}, Reference: "r"},
		{Amount: Money{AmountMinor: 100, Currency: "ugx"}, Reference: "r"},
	}
	for _, request := range cases {
		if _, err := provider.StartPayment(t.Context(), request); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("StartPayment(%+v) error = %v, want ErrInvalidRequest", request, err)
		}
	}
}

func TestFakeVerifyWebhookNeedsTheSharedSecret(t *testing.T) {
	provider := newTestFake(t)
	body := []byte(`{"payment_id":"a2c9c1f0-0000-4000-8000-000000000001"}`)

	headers := http.Header{}
	if _, err := provider.VerifyWebhook(headers, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyWebhook without a secret = %v, want ErrInvalidSignature", err)
	}

	headers.Set(FakeSecretHeader, "not-the-secret")
	if _, err := provider.VerifyWebhook(headers, body); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("VerifyWebhook with a wrong secret = %v, want ErrInvalidSignature", err)
	}

	headers.Set(FakeSecretHeader, "fake-secret-value")
	event, err := provider.VerifyWebhook(headers, body)
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if event.ProviderRef != "a2c9c1f0-0000-4000-8000-000000000001" {
		t.Errorf("ProviderRef = %q", event.ProviderRef)
	}
	if event.Status != StatusPaid {
		t.Errorf("Status = %q, want paid by default", event.Status)
	}
}

func TestFakeVerifyWebhookCarriesAnExplicitStatus(t *testing.T) {
	provider := newTestFake(t)
	headers := http.Header{}
	headers.Set(FakeSecretHeader, "fake-secret-value")

	event, err := provider.VerifyWebhook(headers, []byte(`{"payment_id":"a2c9c1f0-0000-4000-8000-000000000001","status":"failed"}`))
	if err != nil {
		t.Fatalf("VerifyWebhook: %v", err)
	}
	if event.Status != StatusFailed {
		t.Errorf("Status = %q, want failed", event.Status)
	}

	if _, err := provider.VerifyWebhook(headers, []byte(`{"payment_id":"a","status":"nonsense"}`)); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("VerifyWebhook with an unknown status = %v, want ErrInvalidWebhook", err)
	}
	if _, err := provider.VerifyWebhook(headers, []byte(`{"status":"paid"}`)); !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("VerifyWebhook without a payment id = %v, want ErrInvalidWebhook", err)
	}
}

func TestNewFakeRequiresASecret(t *testing.T) {
	if _, err := NewFake(FakeOptions{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("NewFake without a secret = %v, want ErrNotConfigured", err)
	}
}

func TestRegistryLooksUpProvidersByName(t *testing.T) {
	registry := NewRegistry()
	fake := newTestFake(t)

	if err := registry.Register(FakeProviderName, fake); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := registry.Register(FakeProviderName, fake); !errors.Is(err, ErrProviderRegistered) {
		t.Fatalf("duplicate Register = %v, want ErrProviderRegistered", err)
	}

	found, err := registry.Lookup(FakeProviderName)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if found != PaymentProvider(fake) {
		t.Error("Lookup returned a different provider")
	}
	if _, err := registry.Lookup("nylonpay"); !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("Lookup of an unregistered provider = %v, want ErrProviderUnknown", err)
	}
	if _, err := registry.Lookup("  FAKE  "); err != nil {
		t.Fatalf("Lookup is not case and space insensitive: %v", err)
	}

	name, provider, err := registry.Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if name != FakeProviderName || provider != PaymentProvider(fake) {
		t.Errorf("Default = %q, want the only registered provider", name)
	}
	if err := registry.SetDefault("nylonpay"); !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("SetDefault of an unregistered provider = %v, want ErrProviderUnknown", err)
	}
}

func TestEmptyRegistryHasNoDefault(t *testing.T) {
	if _, _, err := NewRegistry().Default(); !errors.Is(err, ErrProviderUnknown) {
		t.Fatalf("Default of an empty registry = %v, want ErrProviderUnknown", err)
	}
}
