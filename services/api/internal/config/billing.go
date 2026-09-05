package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	PaymentProviderNone     = "none"
	PaymentProviderFake     = "fake"
	PaymentProviderNylonPay = "nylonpay"

	DefaultFreeGrantMinutes  = 300
	DefaultCheckoutRateLimit = 10
	MinAdminTokenLength      = 32
	MinPaymentSecretLength   = 16
)

var paymentProviders = []string{PaymentProviderNone, PaymentProviderFake, PaymentProviderNylonPay}

type NylonPayConfig struct {
	BaseURL       string
	APIKey        string
	WebhookSecret string
}

type BillingConfig struct {
	Provider         string
	AllowFake        bool
	CheckoutLimit    int64
	CheckoutWindow   time.Duration
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
		Provider:         reader.choice("PAYMENT_PROVIDER", PaymentProviderNone, paymentProviders),
		AllowFake:        reader.boolean("ALLOW_FAKE_PAYMENTS", false),
		CheckoutLimit:    int64(reader.boundedInt("CHECKOUT_RATE_LIMIT", DefaultCheckoutRateLimit, 1, 10000)),
		CheckoutWindow:   reader.duration("CHECKOUT_RATE_WINDOW", time.Hour),
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

	if cfg.Provider == PaymentProviderFake && !cfg.AllowFake {
		reader.reject("PAYMENT_PROVIDER", "may only be fake when ALLOW_FAKE_PAYMENTS is true")
	}

	if err := reader.err(); err != nil {
		return BillingConfig{}, err
	}
	return cfg, nil
}

func (c BillingConfig) ProviderConfigured() bool {
	switch c.Provider {
	case PaymentProviderNylonPay:
		return c.NylonPayConfigured()
	case PaymentProviderFake:
		return c.FakeConfigured()
	default:
		return false
	}
}

func (c BillingConfig) NylonPayConfigured() bool {
	return c.NylonPay.BaseURL != "" && c.NylonPay.APIKey != "" && c.NylonPay.WebhookSecret != ""
}

func (c BillingConfig) FakeConfigured() bool {
	return c.AllowFake && c.FakeSecret != ""
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
