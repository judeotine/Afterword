package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadBillingDefaults(t *testing.T) {
	cfg, err := LoadBilling(lookupFrom(nil))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if cfg.Provider != PaymentProviderNone {
		t.Errorf("Provider = %q, want none", cfg.Provider)
	}
	if cfg.AllowFake {
		t.Error("AllowFake is true with no ALLOW_FAKE_PAYMENTS")
	}
	if cfg.CheckoutLimit != DefaultCheckoutRateLimit || cfg.CheckoutWindow != time.Hour {
		t.Errorf("checkout limit = %d per %s", cfg.CheckoutLimit, cfg.CheckoutWindow)
	}
	if cfg.PendingTTL != 24*time.Hour {
		t.Errorf("PendingTTL = %s, want 24h", cfg.PendingTTL)
	}
	if cfg.FreeGrantMinutes != DefaultFreeGrantMinutes {
		t.Errorf("FreeGrantMinutes = %d, want %d", cfg.FreeGrantMinutes, DefaultFreeGrantMinutes)
	}
	if cfg.GrantInterval != time.Hour {
		t.Errorf("GrantInterval = %s, want 1h", cfg.GrantInterval)
	}
	if cfg.AdminConfigured() {
		t.Error("AdminConfigured is true with no ADMIN_TOKEN")
	}
	if cfg.ProviderConfigured() {
		t.Error("ProviderConfigured is true with PAYMENT_PROVIDER unset")
	}
}

func TestLoadBillingReadsEveryKey(t *testing.T) {
	cfg, err := LoadBilling(lookupFrom(map[string]string{
		"PAYMENT_PROVIDER":        "nylonpay",
		"ALLOW_FAKE_PAYMENTS":     "true",
		"FREE_GRANT_MINUTES":      "500",
		"GRANT_INTERVAL":          "15m",
		"ADMIN_TOKEN":             strings.Repeat("a", 32),
		"FAKE_PAYMENT_SECRET":     strings.Repeat("f", 16),
		"NYLONPAY_BASE_URL":       "https://pay.example.test/api/",
		"NYLONPAY_API_KEY":        "key_live",
		"NYLONPAY_WEBHOOK_SECRET": strings.Repeat("w", 24),
	}))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if cfg.Provider != PaymentProviderNylonPay {
		t.Errorf("Provider = %q", cfg.Provider)
	}
	if cfg.FreeGrantMinutes != 500 || cfg.GrantInterval != 15*time.Minute {
		t.Errorf("grant settings = %d minutes every %s", cfg.FreeGrantMinutes, cfg.GrantInterval)
	}
	if cfg.NylonPay.BaseURL != "https://pay.example.test/api" {
		t.Errorf("NylonPay.BaseURL = %q, want the trailing slash removed", cfg.NylonPay.BaseURL)
	}
	if !cfg.ProviderConfigured() || !cfg.NylonPayConfigured() || !cfg.FakeConfigured() || !cfg.AdminConfigured() {
		t.Error("a fully populated billing configuration did not report as configured")
	}
}

func TestLoadBillingRejectsWeakSecretsAndBadValues(t *testing.T) {
	cases := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{"short admin token", map[string]string{"ADMIN_TOKEN": "short"}, "ADMIN_TOKEN"},
		{"short fake secret", map[string]string{"FAKE_PAYMENT_SECRET": "tiny"}, "FAKE_PAYMENT_SECRET"},
		{"short webhook secret", map[string]string{"NYLONPAY_WEBHOOK_SECRET": "tiny"}, "NYLONPAY_WEBHOOK_SECRET"},
		{"unknown provider", map[string]string{"PAYMENT_PROVIDER": "stripe"}, "PAYMENT_PROVIDER"},
		{"fake without the flag", map[string]string{"PAYMENT_PROVIDER": "fake", "FAKE_PAYMENT_SECRET": strings.Repeat("f", 16)}, "PAYMENT_PROVIDER"},
		{"negative grant", map[string]string{"FREE_GRANT_MINUTES": "-1"}, "FREE_GRANT_MINUTES"},
		{"bad interval", map[string]string{"GRANT_INTERVAL": "never"}, "GRANT_INTERVAL"},
		{"relative base url", map[string]string{"NYLONPAY_BASE_URL": "/payments"}, "NYLONPAY_BASE_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadBilling(lookupFrom(tc.values))
			if err == nil {
				t.Fatalf("LoadBilling(%v) returned no error", tc.values)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %s", err, tc.want)
			}
		})
	}
}

func TestZeroFreeGrantIsAllowed(t *testing.T) {
	cfg, err := LoadBilling(lookupFrom(map[string]string{"FREE_GRANT_MINUTES": "0"}))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if cfg.FreeGrantMinutes != 0 {
		t.Fatalf("FreeGrantMinutes = %d, want 0", cfg.FreeGrantMinutes)
	}
}

func TestTheFakeProviderNeedsItsFlagAndItsSecret(t *testing.T) {
	secretOnly, err := LoadBilling(lookupFrom(map[string]string{"FAKE_PAYMENT_SECRET": strings.Repeat("f", 16)}))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if secretOnly.FakeConfigured() {
		t.Error("the fake provider is configured without ALLOW_FAKE_PAYMENTS")
	}

	flagOnly, err := LoadBilling(lookupFrom(map[string]string{"ALLOW_FAKE_PAYMENTS": "true"}))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if flagOnly.FakeConfigured() {
		t.Error("the fake provider is configured without a secret")
	}

	both, err := LoadBilling(lookupFrom(map[string]string{
		"PAYMENT_PROVIDER":    "fake",
		"ALLOW_FAKE_PAYMENTS": "true",
		"FAKE_PAYMENT_SECRET": strings.Repeat("f", 16),
	}))
	if err != nil {
		t.Fatalf("LoadBilling: %v", err)
	}
	if !both.FakeConfigured() || !both.ProviderConfigured() {
		t.Error("the fake provider is not configured with both its flag and its secret")
	}
}
