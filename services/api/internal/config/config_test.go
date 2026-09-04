package config

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func baseEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://afterword:afterword@localhost:5432/afterword?sslmode=disable",
		"JWT_SECRET":   "0123456789abcdef0123456789abcdef",
	}
}

func lookupFrom(env map[string]string) LookupFunc {
	return func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(baseEnv()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.AppBaseURL != "http://localhost:3000" {
		t.Errorf("AppBaseURL = %q, want %q", cfg.AppBaseURL, "http://localhost:3000")
	}
	if cfg.APIBaseURL != "http://localhost:8080" {
		t.Errorf("APIBaseURL = %q, want %q", cfg.APIBaseURL, "http://localhost:8080")
	}
	if cfg.S3.Region != "us-east-1" {
		t.Errorf("S3.Region = %q, want %q", cfg.S3.Region, "us-east-1")
	}
	if cfg.S3.UseSSL {
		t.Error("S3.UseSSL = true, want false")
	}
	if cfg.ShutdownTimeout != 15*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 15s", cfg.ShutdownTimeout)
	}
	if cfg.RequestTimeout != 30*time.Second {
		t.Errorf("RequestTimeout = %s, want 30s", cfg.RequestTimeout)
	}
	if cfg.DatabaseMaxConns != 10 {
		t.Errorf("DatabaseMaxConns = %d, want 10", cfg.DatabaseMaxConns)
	}
	if cfg.AbandonedUploadTTL != 24*time.Hour {
		t.Errorf("AbandonedUploadTTL = %s, want 24h", cfg.AbandonedUploadTTL)
	}
}

func TestLoadReadsProvidedValues(t *testing.T) {
	env := baseEnv()
	env["PORT"] = "9091"
	env["LOG_LEVEL"] = "DEBUG"
	env["APP_BASE_URL"] = "https://app.afterword.io/"
	env["API_BASE_URL"] = "https://api.afterword.io"
	env["S3_ENDPOINT"] = "https://minio.internal:9000"
	env["S3_ACCESS_KEY"] = "access"
	env["S3_SECRET_KEY"] = "secret"
	env["S3_REGION"] = "eu-central-1"
	env["S3_USE_SSL"] = "true"
	env["DATABASE_MAX_CONNS"] = "24"
	env["ABANDONED_UPLOAD_TTL"] = "6h"
	env["SHUTDOWN_TIMEOUT"] = "5s"
	env["REQUEST_TIMEOUT"] = "45s"

	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Port != 9091 {
		t.Errorf("Port = %d, want 9091", cfg.Port)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.AppBaseURL != "https://app.afterword.io" {
		t.Errorf("AppBaseURL = %q, want trailing slash trimmed", cfg.AppBaseURL)
	}
	if cfg.S3.Endpoint != "https://minio.internal:9000" {
		t.Errorf("S3.Endpoint = %q", cfg.S3.Endpoint)
	}
	if cfg.S3.AccessKey != "access" || cfg.S3.SecretKey != "secret" {
		t.Errorf("S3 credentials not read: %+v", cfg.S3)
	}
	if !cfg.S3.UseSSL {
		t.Error("S3.UseSSL = false, want true")
	}
	if cfg.DatabaseMaxConns != 24 {
		t.Errorf("DatabaseMaxConns = %d, want 24", cfg.DatabaseMaxConns)
	}
	if cfg.AbandonedUploadTTL != 6*time.Hour {
		t.Errorf("AbandonedUploadTTL = %s, want 6h", cfg.AbandonedUploadTTL)
	}
	if cfg.ShutdownTimeout != 5*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 5s", cfg.ShutdownTimeout)
	}
	if cfg.RequestTimeout != 45*time.Second {
		t.Errorf("RequestTimeout = %s, want 45s", cfg.RequestTimeout)
	}
}

func TestLoadAddressUsesPort(t *testing.T) {
	env := baseEnv()
	env["PORT"] = "9000"
	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Address() != ":9000" {
		t.Errorf("Address() = %q, want %q", cfg.Address(), ":9000")
	}
}

func TestLoadRejectsMissingRequiredValues(t *testing.T) {
	for _, key := range []string{"DATABASE_URL", "JWT_SECRET"} {
		t.Run(key, func(t *testing.T) {
			env := baseEnv()
			delete(env, key)
			_, err := Load(lookupFrom(env))
			if err == nil {
				t.Fatalf("Load succeeded without %s", key)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name %s", err, key)
			}
		})
	}
}

func TestLoadRejectsBlankRequiredValues(t *testing.T) {
	env := baseEnv()
	env["JWT_SECRET"] = "   "
	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("Load succeeded with a blank JWT_SECRET")
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	env := baseEnv()
	env["JWT_SECRET"] = "tooshort"
	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("Load succeeded with a short JWT_SECRET")
	}
	if !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Errorf("error %q does not name JWT_SECRET", err)
	}
}

func TestLoadRejectsInvalidValues(t *testing.T) {
	cases := map[string]string{
		"PORT":               "not-a-number",
		"DATABASE_MAX_CONNS": "0",
		"S3_USE_SSL":         "perhaps",
		"LOG_LEVEL":          "chatty",
		"APP_BASE_URL":       "app.afterword.io",
		"API_BASE_URL":       "://broken",
		"SHUTDOWN_TIMEOUT":   "soon",
		"DATABASE_URL":       "mysql://localhost/afterword",
	}
	for key, value := range cases {
		t.Run(key, func(t *testing.T) {
			env := baseEnv()
			env[key] = value
			_, err := Load(lookupFrom(env))
			if err == nil {
				t.Fatalf("Load succeeded with %s=%q", key, value)
			}
			if !strings.Contains(err.Error(), key) {
				t.Errorf("error %q does not name %s", err, key)
			}
		})
	}
}

func TestLoadRejectsOutOfRangePort(t *testing.T) {
	for _, value := range []string{"0", "70000", "-1"} {
		env := baseEnv()
		env["PORT"] = value
		if _, err := Load(lookupFrom(env)); err == nil {
			t.Errorf("Load succeeded with PORT=%q", value)
		}
	}
}

func TestLoadReportsEveryProblemAtOnce(t *testing.T) {
	env := map[string]string{"PORT": "nope"}
	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("Load succeeded with an empty environment")
	}
	var invalid *Error
	if !errors.As(err, &invalid) {
		t.Fatalf("error %T is not *config.Error", err)
	}
	if len(invalid.Problems) != 3 {
		t.Fatalf("Problems = %v, want one per broken variable", invalid.Problems)
	}
}

func TestFromEnvironmentUsesProcessEnvironment(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://localhost:5432/afterword")
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("PORT", "8123")
	cfg, err := FromEnvironment()
	if err != nil {
		t.Fatalf("FromEnvironment returned error: %v", err)
	}
	if cfg.Port != 8123 {
		t.Errorf("Port = %d, want 8123", cfg.Port)
	}
}

func TestLoadParsesTrustedProxies(t *testing.T) {
	env := baseEnv()
	env["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, 172.18.0.4"
	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.TrustedProxies) != 2 {
		t.Fatalf("TrustedProxies = %v, want two entries", cfg.TrustedProxies)
	}
}

func TestLoadDefaultsToTrustingNoProxies(t *testing.T) {
	cfg, err := Load(lookupFrom(baseEnv()))
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(cfg.TrustedProxies) != 0 {
		t.Errorf("TrustedProxies = %v, want none by default", cfg.TrustedProxies)
	}
}

func TestLoadRejectsInvalidTrustedProxies(t *testing.T) {
	env := baseEnv()
	env["TRUSTED_PROXY_CIDRS"] = "10.0.0.0/8, not-an-ip"
	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("Load succeeded with an invalid TRUSTED_PROXY_CIDRS")
	}
	if !strings.Contains(err.Error(), "TRUSTED_PROXY_CIDRS") {
		t.Errorf("error %q does not name TRUSTED_PROXY_CIDRS", err)
	}
}

func TestAuthDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://afterword:afterword@localhost:5432/afterword",
		"JWT_SECRET":   "0123456789abcdef0123456789abcdef",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.EmailSender != "log" || cfg.Auth.SMSSender != "log" {
		t.Fatalf("sender defaults = %q / %q", cfg.Auth.EmailSender, cfg.Auth.SMSSender)
	}
	if cfg.Auth.SMTP.Port != 587 || !cfg.Auth.SMTP.StartTLS {
		t.Fatalf("smtp defaults = %+v", cfg.Auth.SMTP)
	}
	if cfg.Auth.GoogleRedirectURL != "http://localhost:8080/v1/auth/google/callback" {
		t.Fatalf("GoogleRedirectURL = %q", cfg.Auth.GoogleRedirectURL)
	}
	if cfg.GoogleConfigured() {
		t.Fatal("GoogleConfigured() is true without credentials")
	}
}

func TestAuthValues(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL":         "postgres://afterword:afterword@localhost:5432/afterword",
		"JWT_SECRET":           "0123456789abcdef0123456789abcdef",
		"API_BASE_URL":         "https://api.afterword.app",
		"GOOGLE_CLIENT_ID":     "client-id",
		"GOOGLE_CLIENT_SECRET": "client-secret",
		"EMAIL_SENDER":         "SMTP",
		"SMS_SENDER":           "noop",
		"SMTP_HOST":            "smtp.example.com",
		"SMTP_PORT":            "465",
		"SMTP_FROM":            "Afterword <hello@example.com>",
		"SMTP_STARTTLS":        "false",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Auth.EmailSender != "smtp" || cfg.Auth.SMSSender != "noop" {
		t.Fatalf("senders = %q / %q", cfg.Auth.EmailSender, cfg.Auth.SMSSender)
	}
	if cfg.Auth.SMTP.Port != 465 || cfg.Auth.SMTP.StartTLS {
		t.Fatalf("smtp = %+v", cfg.Auth.SMTP)
	}
	if cfg.Auth.GoogleRedirectURL != "https://api.afterword.app/v1/auth/google/callback" {
		t.Fatalf("GoogleRedirectURL = %q", cfg.Auth.GoogleRedirectURL)
	}
	if !cfg.GoogleConfigured() {
		t.Fatal("GoogleConfigured() is false with credentials")
	}
}

func TestAuthRejectsIncompleteConfiguration(t *testing.T) {
	cases := []map[string]string{
		{"EMAIL_SENDER": "smtp"},
		{"EMAIL_SENDER": "smtp", "SMTP_HOST": "smtp.example.com"},
		{"EMAIL_SENDER": "carrier-pigeon"},
		{"SMS_SENDER": "twilio"},
		{"GOOGLE_CLIENT_ID": "client-id"},
		{"GOOGLE_CLIENT_SECRET": "client-secret"},
		{"SMTP_PORT": "0"},
	}
	for _, extra := range cases {
		values := map[string]string{
			"DATABASE_URL": "postgres://afterword:afterword@localhost:5432/afterword",
			"JWT_SECRET":   "0123456789abcdef0123456789abcdef",
		}
		for key, value := range extra {
			values[key] = value
		}
		if _, err := Load(lookupFrom(values)); err == nil {
			t.Fatalf("Load(%v) accepted an incomplete configuration", extra)
		}
	}
}

func TestStorageDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(baseEnv()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.S3.AudioBucket != "audio" || cfg.S3.TranscriptsBucket != "transcripts" {
		t.Fatalf("unexpected buckets: %+v", cfg.S3)
	}
	if cfg.S3.ClipsBucket != "clips" || cfg.S3.ExportsBucket != "exports" {
		t.Fatalf("unexpected buckets: %+v", cfg.S3)
	}
	if !cfg.S3.UsePathStyle {
		t.Fatal("path style addressing should be the default for MinIO")
	}
	if cfg.S3.UploadTTL != 30*time.Minute || cfg.S3.DownloadTTL != 15*time.Minute {
		t.Fatalf("unexpected presign windows: %v %v", cfg.S3.UploadTTL, cfg.S3.DownloadTTL)
	}
	if cfg.S3.MaxAudioBytes != 2048<<20 {
		t.Fatalf("max audio bytes %d", cfg.S3.MaxAudioBytes)
	}
	if cfg.StorageConfigured() {
		t.Fatal("storage reported itself configured without credentials")
	}
}

func TestStorageOverrides(t *testing.T) {
	env := baseEnv()
	env["S3_ACCESS_KEY"] = "key"
	env["S3_SECRET_KEY"] = "secret"
	env["S3_BUCKET_AUDIO"] = "Recordings"
	env["S3_BUCKET_TRANSCRIPTS"] = "notes"
	env["S3_USE_PATH_STYLE"] = "false"
	env["S3_MAX_AUDIO_MB"] = "64"

	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.S3.AudioBucket != "recordings" || cfg.S3.TranscriptsBucket != "notes" {
		t.Fatalf("unexpected buckets: %+v", cfg.S3)
	}
	if cfg.S3.UsePathStyle {
		t.Fatal("path style addressing was not disabled")
	}
	if cfg.S3.MaxAudioBytes != 64<<20 {
		t.Fatalf("max audio bytes %d", cfg.S3.MaxAudioBytes)
	}
	if !cfg.StorageConfigured() {
		t.Fatal("storage should be configured once credentials are present")
	}
}

func TestStorageRejectsABadBucketName(t *testing.T) {
	env := baseEnv()
	env["S3_BUCKET_CLIPS"] = "Not A Bucket!"

	_, err := Load(lookupFrom(env))
	if err == nil {
		t.Fatal("a malformed bucket name was accepted")
	}
	if !strings.Contains(err.Error(), "S3_BUCKET_CLIPS") {
		t.Fatalf("error %v does not name the variable", err)
	}
}

func TestLoadRejectsAPoolBelowTheSchedulerFloor(t *testing.T) {
	env := baseEnv()
	env["DATABASE_MAX_CONNS"] = "4"
	if _, err := Load(lookupFrom(env)); err == nil {
		t.Fatal("Load succeeded with DATABASE_MAX_CONNS=4")
	}

	env["DATABASE_MAX_CONNS"] = "5"
	cfg, err := Load(lookupFrom(env))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseMaxConns != 5 {
		t.Errorf("DatabaseMaxConns = %d, want 5", cfg.DatabaseMaxConns)
	}
}
