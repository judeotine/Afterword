package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type LookupFunc func(key string) (string, bool)

type S3Config struct {
	Endpoint           string
	AccessKey          string
	SecretKey          string
	Region             string
	UseSSL             bool
	UsePathStyle       bool
	AudioBucket        string
	TranscriptsBucket  string
	ClipsBucket        string
	ExportsBucket      string
	UploadTTL          time.Duration
	DownloadTTL        time.Duration
	MaxAudioBytes      int64
	MaxTranscriptBytes int64
}

type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	StartTLS bool
}

type AuthConfig struct {
	GoogleClientID     string
	GoogleClientSecret string
	GoogleRedirectURL  string
	EmailSender        string
	SMSSender          string
	SMTP               SMTPConfig
}

type Config struct {
	Port               int
	DatabaseURL        string
	DatabaseMaxConns   int32
	JWTSecret          string
	AppBaseURL         string
	APIBaseURL         string
	LogLevel           string
	S3                 S3Config
	Auth               AuthConfig
	TrustedProxies     []netip.Prefix
	ShareRateWindow    time.Duration
	ShareRateLimit     int64
	AbandonedUploadTTL time.Duration
	RequestTimeout     time.Duration
	ShutdownTimeout    time.Duration
}

type Error struct {
	Problems []string
}

func (e *Error) Error() string {
	return "invalid configuration: " + strings.Join(e.Problems, "; ")
}

func (c Config) Address() string {
	return ":" + strconv.Itoa(c.Port)
}

func FromEnvironment() (Config, error) {
	return Load(os.LookupEnv)
}

func Load(lookup LookupFunc) (Config, error) {
	reader := &reader{lookup: lookup, problems: map[string]string{}}

	cfg := Config{
		Port:             reader.port("PORT", 8080),
		DatabaseURL:      reader.databaseURL("DATABASE_URL"),
		DatabaseMaxConns: int32(reader.boundedInt("DATABASE_MAX_CONNS", 10, 10, 500)),
		JWTSecret:        reader.secret("JWT_SECRET", 32),
		AppBaseURL:       reader.baseURL("APP_BASE_URL", "http://localhost:3000"),
		APIBaseURL:       reader.baseURL("API_BASE_URL", "http://localhost:8080"),
		LogLevel:         reader.logLevel("LOG_LEVEL", "info"),
		S3: S3Config{
			Endpoint:           reader.optional("S3_ENDPOINT", "http://localhost:9000"),
			AccessKey:          reader.optional("S3_ACCESS_KEY", ""),
			SecretKey:          reader.optional("S3_SECRET_KEY", ""),
			Region:             reader.optional("S3_REGION", "us-east-1"),
			UseSSL:             reader.boolean("S3_USE_SSL", false),
			UsePathStyle:       reader.boolean("S3_USE_PATH_STYLE", true),
			AudioBucket:        reader.bucket("S3_BUCKET_AUDIO", "audio"),
			TranscriptsBucket:  reader.bucket("S3_BUCKET_TRANSCRIPTS", "transcripts"),
			ClipsBucket:        reader.bucket("S3_BUCKET_CLIPS", "clips"),
			ExportsBucket:      reader.bucket("S3_BUCKET_EXPORTS", "exports"),
			UploadTTL:          reader.duration("S3_UPLOAD_TTL", 30*time.Minute),
			DownloadTTL:        reader.duration("S3_DOWNLOAD_TTL", 15*time.Minute),
			MaxAudioBytes:      int64(reader.boundedInt("S3_MAX_AUDIO_MB", 2048, 1, 102400)) << 20,
			MaxTranscriptBytes: int64(reader.boundedInt("S3_MAX_TRANSCRIPT_MB", 64, 1, 4096)) << 20,
		},
		Auth: AuthConfig{
			GoogleClientID:     reader.optional("GOOGLE_CLIENT_ID", ""),
			GoogleClientSecret: reader.optional("GOOGLE_CLIENT_SECRET", ""),
			EmailSender:        reader.choice("EMAIL_SENDER", "log", emailSenders),
			SMSSender:          reader.choice("SMS_SENDER", "log", smsSenders),
			SMTP: SMTPConfig{
				Host:     reader.optional("SMTP_HOST", ""),
				Port:     reader.boundedInt("SMTP_PORT", 587, 1, 65535),
				Username: reader.optional("SMTP_USERNAME", ""),
				Password: reader.optional("SMTP_PASSWORD", ""),
				From:     reader.optional("SMTP_FROM", ""),
				StartTLS: reader.boolean("SMTP_STARTTLS", true),
			},
		},
		TrustedProxies:     reader.trustedProxies("TRUSTED_PROXY_CIDRS"),
		ShareRateWindow:    reader.duration("SHARE_RATE_WINDOW", time.Minute),
		ShareRateLimit:     int64(reader.boundedInt("SHARE_RATE_LIMIT", 120, 1, 100000)),
		AbandonedUploadTTL: reader.duration("ABANDONED_UPLOAD_TTL", 24*time.Hour),
		RequestTimeout:     reader.duration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout:    reader.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	cfg.Auth.GoogleRedirectURL = reader.baseURL("GOOGLE_REDIRECT_URL", cfg.APIBaseURL+googleCallbackPath)

	if cfg.Auth.EmailSender == emailSenderSMTP {
		if cfg.Auth.SMTP.Host == "" {
			reader.reject("SMTP_HOST", "is required when EMAIL_SENDER is smtp")
		}
		if cfg.Auth.SMTP.From == "" {
			reader.reject("SMTP_FROM", "is required when EMAIL_SENDER is smtp")
		}
	}
	if (cfg.Auth.GoogleClientID == "") != (cfg.Auth.GoogleClientSecret == "") {
		reader.reject("GOOGLE_CLIENT_SECRET", "and GOOGLE_CLIENT_ID must be set together")
	}

	if err := reader.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

const (
	googleCallbackPath = "/v1/auth/google/callback"
	emailSenderLog     = "log"
	emailSenderSMTP    = "smtp"
	smsSenderLog       = "log"
	smsSenderNoop      = "noop"
)

var (
	emailSenders = []string{emailSenderLog, emailSenderSMTP}
	smsSenders   = []string{smsSenderLog, smsSenderNoop}
)

func (c Config) StorageConfigured() bool {
	return c.S3.Endpoint != "" && c.S3.AccessKey != "" && c.S3.SecretKey != ""
}

func (c Config) GoogleConfigured() bool {
	return c.Auth.GoogleClientID != "" && c.Auth.GoogleClientSecret != ""
}

type reader struct {
	lookup   LookupFunc
	problems map[string]string
}

func (r *reader) err() error {
	if len(r.problems) == 0 {
		return nil
	}
	keys := make([]string, 0, len(r.problems))
	for key := range r.problems {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	messages := make([]string, 0, len(keys))
	for _, key := range keys {
		messages = append(messages, r.problems[key])
	}
	return &Error{Problems: messages}
}

func (r *reader) reject(key, reason string) {
	r.problems[key] = key + " " + reason
}

func (r *reader) raw(key string) (string, bool) {
	value, ok := r.lookup(key)
	if !ok {
		return "", false
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	return value, true
}

func (r *reader) optional(key, fallback string) string {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	return value
}

func (r *reader) required(key string) (string, bool) {
	value, ok := r.raw(key)
	if !ok {
		r.reject(key, "is required")
		return "", false
	}
	return value, true
}

func (r *reader) secret(key string, minLength int) string {
	value, ok := r.required(key)
	if !ok {
		return ""
	}
	if len(value) < minLength {
		r.reject(key, fmt.Sprintf("must be at least %d characters", minLength))
		return ""
	}
	return value
}

func (r *reader) port(key string, fallback int) int {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		r.reject(key, "must be a number between 1 and 65535")
		return fallback
	}
	if parsed < 1 || parsed > 65535 {
		r.reject(key, "must be a number between 1 and 65535")
		return fallback
	}
	return parsed
}

func (r *reader) boundedInt(key string, fallback, minimum, maximum int) int {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < minimum || parsed > maximum {
		r.reject(key, fmt.Sprintf("must be a number between %d and %d", minimum, maximum))
		return fallback
	}
	return parsed
}

func (r *reader) boolean(key string, fallback bool) bool {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		r.reject(key, "must be true or false")
		return fallback
	}
	return parsed
}

func (r *reader) duration(key string, fallback time.Duration) time.Duration {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		r.reject(key, "must be a positive duration such as 30s")
		return fallback
	}
	return parsed
}

var logLevels = map[string]struct{}{
	"trace": {}, "debug": {}, "info": {}, "warn": {}, "error": {}, "fatal": {}, "panic": {},
}

func (r *reader) logLevel(key, fallback string) string {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	level := strings.ToLower(value)
	if _, known := logLevels[level]; !known {
		r.reject(key, "must be one of trace, debug, info, warn, error, fatal, panic")
		return fallback
	}
	return level
}

func (r *reader) choice(key, fallback string, allowed []string) string {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	candidate := strings.ToLower(value)
	for _, option := range allowed {
		if candidate == option {
			return candidate
		}
	}
	r.reject(key, "must be one of "+strings.Join(allowed, ", "))
	return fallback
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)

func (r *reader) bucket(key, fallback string) string {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	name := strings.ToLower(value)
	if !bucketPattern.MatchString(name) {
		r.reject(key, "must be a valid bucket name of lowercase letters, numbers, dots and dashes")
		return fallback
	}
	return name
}

func (r *reader) baseURL(key, fallback string) string {
	value, ok := r.raw(key)
	if !ok {
		return fallback
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		r.reject(key, "must be an absolute http or https URL")
		return fallback
	}
	return strings.TrimRight(value, "/")
}

func (r *reader) databaseURL(key string) string {
	value, ok := r.required(key)
	if !ok {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		r.reject(key, "must be a postgres:// connection string")
		return ""
	}
	return value
}

func (r *reader) trustedProxies(key string) []netip.Prefix {
	value, ok := r.raw(key)
	if !ok {
		return nil
	}
	prefixes, err := httpx.ParseTrustedProxies(value)
	if err != nil {
		r.reject(key, "must be a comma separated list of IP addresses or CIDR blocks")
		return nil
	}
	return prefixes
}
