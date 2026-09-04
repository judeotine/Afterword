package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	PaymentProviderFake     = "fake"
	PaymentProviderNylonPay = "nylonpay"

	DefaultFreeGrantMinutes = 300
	MinAdminTokenLength     = 32
	MinPaymentSecretLength  = 16
)

var paymentProviders = []string{PaymentProviderFake, PaymentProviderNylonPay}

type NylonPayConfig struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
}

type BillingConfig struct {
	Provider         string
	FreeGrantMinutes int32
	GrantInterval    time.Duration
	AdminToken       string
	PendingTTL       time.Duration
	FakeSecret       string
	NylonPay         NylonPayConfig
}

func LoadBilling(lookup LookupFunc) (BillingConfig, error) {
	reader := &reader{lookup: lookup, problems: map[string]string{}}

	cfg := BillingConfig{
		Provider:         reader.choice("PAYMENT_PROVIDER", PaymentProviderFake, paymentProviders),
		FreeGrantMinutes: int32(reader.boundedInt("FREE_GRANT_MINUTES", DefaultFreeGrantMinutes, 0, 1_000_000)),
		GrantInterval:    reader.duration("GRANT_INTERVAL", time.Hour),
		AdminToken:       reader.optionalSecret("ADMIN_TOKEN", MinAdminTokenLength),
		PendingTTL:       reader.duration("PAYMENT_PENDING_TTL", 24*time.Hour),
		FakeSecret:       reader.optionalSecret("FAKE_PAYMENT_SECRET", MinPaymentSecretLength),
		NylonPay: NylonPayConfig{
			BaseURL:       reader.optionalBaseURL("NYLONPAY_BASE_URL"),
			APIKey:        reader.optional("NYLONPAY_API_KEY", ""),
			WebhookSecret: reader.optionalSecret("NYLONPAY_WEBHOOK_SECRET", MinPaymentSecretLength),
		},
	}

	if err := reader.err(); err != nil {
		return BillingConfig{}, err
	}
	return cfg, nil
}

func (c BillingConfig) ProviderConfigured() bool {
	switch c.Provider {
	case PaymentProviderNylonPay:
		return c.NylonPay.BaseURL != "" && c.NylonPay.APIKey != "" && c.NylonPay.WebhookSecret != ""
	default:
		return c.FakeSecret != ""
	}
}

func (c BillingConfig) NylonPayConfigured() bool {
	return c.NylonPay.BaseURL != "" && c.NylonPay.APIKey != "" && c.NylonPay.WebhookSecret != ""
}

func (c BillingConfig) FakeConfigured() bool {
	return c.FakeSecret != ""
}

func (c BillingConfig) AdminConfigured() bool {
	return strings.TrimSpace(c.AdminToken) != ""
}

func (r *reader) optionalSecret(key string, minLength int) string {
	value, ok := r.raw(key)
	if !ok {
		return ""
	}
	if len(value) < minLength {
		r.reject(key, "must be at least "+strconv.Itoa(minLength)+" characters")
		return ""
	}
	return value
}

func (r *reader) optionalBaseURL(key string) string {
	if _, ok := r.raw(key); !ok {
		return ""
	}
	return r.baseURL(key, "")
}

func BillingFromEnvironment() (BillingConfig, error) {
	return LoadBilling(os.LookupEnv)
}
