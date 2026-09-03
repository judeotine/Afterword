package httpx

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
	"github.com/rs/zerolog"
)

type RouterOptions struct {
	Logger         zerolog.Logger
	DB             Pinger
	Metrics        *Metrics
	AllowedOrigin  string
	TrustedProxies []netip.Prefix
	RequestTimeout time.Duration
	PingTimeout    time.Duration
	Mount          func(r chi.Router)
}

const defaultPingTimeout = 2 * time.Second

func NewRouter(opts RouterOptions) http.Handler {
	if opts.Metrics == nil {
		opts.Metrics = NewMetrics()
	}
	if opts.PingTimeout <= 0 {
		opts.PingTimeout = defaultPingTimeout
	}

	router := chi.NewRouter()
	router.NotFound(NotFoundHandler())
	router.MethodNotAllowed(MethodNotAllowedHandler())

	router.Use(RequestID)
	router.Use(RealIP(opts.TrustedProxies))
	router.Use(LoggerContext(opts.Logger))
	router.Use(RequestIDContext)
	router.Use(AccessLog(map[string]bool{"/healthz": true, "/metrics": true}))
	router.Use(Recover)
	router.Use(opts.Metrics.Middleware)
	router.Use(corsMiddleware(opts.AllowedOrigin))

	router.Get("/healthz", Health(opts.DB, opts.PingTimeout))
	router.Method(http.MethodGet, "/metrics", opts.Metrics.Handler())

	if opts.Mount != nil {
		opts.Mount(router)
	}

	var handler http.Handler = router
	if opts.RequestTimeout > 0 {
		handler = Timeout(opts.RequestTimeout)(handler)
	}
	return handler
}

func corsMiddleware(origin string) func(http.Handler) http.Handler {
	options := cors.Options{
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-Id", "X-Workspace-Id", "Idempotency-Key"},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}
	if origin == "" {
		options.AllowOriginFunc = denyEveryOrigin
	} else {
		options.AllowedOrigins = []string{origin}
	}
	return cors.Handler(options)
}

func denyEveryOrigin(*http.Request, string) bool {
	return false
}
