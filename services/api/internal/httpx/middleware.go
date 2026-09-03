package httpx

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/hlog"
)

func LoggerContext(logger zerolog.Logger) func(http.Handler) http.Handler {
	return hlog.NewHandler(logger)
}

const RequestIDHeader = "X-Request-Id"

func RequestIDContext(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := middleware.GetReqID(r.Context()); id != "" {
			w.Header().Set(RequestIDHeader, id)
			hlog.FromRequest(r).UpdateContext(func(c zerolog.Context) zerolog.Context {
				return c.Str("request_id", id)
			})
		}
		next.ServeHTTP(w, r)
	})
}

func AccessLog(skip map[string]bool) func(http.Handler) http.Handler {
	return hlog.AccessHandler(func(r *http.Request, status, size int, duration time.Duration) {
		if skip[r.URL.Path] && status < http.StatusInternalServerError {
			return
		}
		event := hlog.FromRequest(r).Info()
		if status >= http.StatusInternalServerError {
			event = hlog.FromRequest(r).Error()
		} else if status >= http.StatusBadRequest {
			event = hlog.FromRequest(r).Warn()
		}
		event.
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Str("route", routePattern(r)).
			Int("status", status).
			Int("bytes", size).
			Dur("duration_ms", duration).
			Str("remote_ip", r.RemoteAddr).
			Str("user_agent", r.UserAgent()).
			Msg("http request")
	})
}

func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler {
				panic(recovered)
			}
			hlog.FromRequest(r).Error().
				Interface("panic", recovered).
				Bytes("stack", stack()).
				Msg("recovered from panic")
			WriteError(w, r, http.StatusInternalServerError, CodeInternalError, "Something went wrong.")
		}()
		next.ServeHTTP(w, r)
	})
}

func Timeout(d time.Duration) func(http.Handler) http.Handler {
	body, err := marshalErrorEnvelope(CodeRequestTimeout, "The request took too long to complete.")
	if err != nil {
		body = []byte(`{"error":{"code":"request_timeout","message":"The request took too long to complete."}}`)
	}
	return func(next http.Handler) http.Handler {
		timed := http.TimeoutHandler(next, d, string(body))
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			timed.ServeHTTP(w, r)
		})
	}
}
