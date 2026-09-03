package config

import (
	"fmt"
	"net/netip"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type LookupFunc func(key string) (string, bool)

type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Region    string
	UseSSL    bool
}

type Config struct {
	Port             int
	DatabaseURL      string
	DatabaseMaxConns int32
	JWTSecret        string
	AppBaseURL       string
	APIBaseURL       string
	LogLevel         string
	S3               S3Config
	TrustedProxies   []netip.Prefix
	RequestTimeout   time.Duration
	ShutdownTimeout  time.Duration
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
		DatabaseMaxConns: int32(reader.boundedInt("DATABASE_MAX_CONNS", 10, 1, 500)),
		JWTSecret:        reader.secret("JWT_SECRET", 32),
		AppBaseURL:       reader.baseURL("APP_BASE_URL", "http://localhost:3000"),
		APIBaseURL:       reader.baseURL("API_BASE_URL", "http://localhost:8080"),
		LogLevel:         reader.logLevel("LOG_LEVEL", "info"),
		S3: S3Config{
			Endpoint:  reader.optional("S3_ENDPOINT", "http://localhost:9000"),
			AccessKey: reader.optional("S3_ACCESS_KEY", ""),
			SecretKey: reader.optional("S3_SECRET_KEY", ""),
			Region:    reader.optional("S3_REGION", "us-east-1"),
			UseSSL:    reader.boolean("S3_USE_SSL", false),
		},
		TrustedProxies:  reader.trustedProxies("TRUSTED_PROXY_CIDRS"),
		RequestTimeout:  reader.duration("REQUEST_TIMEOUT", 30*time.Second),
		ShutdownTimeout: reader.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
	}

	if err := reader.err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
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
