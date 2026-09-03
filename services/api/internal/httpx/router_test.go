package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
)

func newTestRouter(t *testing.T, pinger Pinger) http.Handler {
	t.Helper()
	return NewRouter(RouterOptions{
		Logger:         zerolog.New(io.Discard),
		DB:             pinger,
		Metrics:        NewMetrics(),
		AllowedOrigin:  "https://app.afterword.io",
		RequestTimeout: time.Second,
	})
}

func TestRouterServesHealthz(t *testing.T) {
	recorder := httptest.NewRecorder()
	newTestRouter(t, &stubPinger{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"db":"up"`) {
		t.Errorf("body = %s", recorder.Body.String())
	}
}

func TestRouterHealthzFailsWhenDatabaseIsDown(t *testing.T) {
	recorder := httptest.NewRecorder()
	newTestRouter(t, &stubPinger{err: errors.New("down")}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestRouterExposesPrometheusMetrics(t *testing.T) {
	router := newTestRouter(t, &stubPinger{})
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	body := recorder.Body.String()
	for _, want := range []string{"go_goroutines", "http_requests_total", "http_request_duration_seconds", "afterword_api_build_info"} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics output is missing %q", want)
		}
	}
	if !strings.Contains(body, `route="/healthz"`) {
		t.Errorf("metrics output does not label the chi route pattern: %s", body)
	}
}

func TestRouterSetsRequestIDHeader(t *testing.T) {
	recorder := httptest.NewRecorder()
	newTestRouter(t, &stubPinger{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id header is empty")
	}
}

func TestRouterUnknownRouteReturnsErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	newTestRouter(t, &stubPinger{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nope", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", recorder.Code)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"error":{"code":"not_found"`) {
		t.Errorf("body = %s", got)
	}
}

func TestRouterMethodNotAllowedReturnsErrorEnvelope(t *testing.T) {
	recorder := httptest.NewRecorder()
	newTestRouter(t, &stubPinger{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"method_not_allowed"`) {
		t.Errorf("body = %s", got)
	}
}

func TestRouterAllowsOnlyTheConfiguredOrigin(t *testing.T) {
	router := newTestRouter(t, &stubPinger{})

	allowed := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	allowed.Header.Set("Origin", "https://app.afterword.io")
	allowed.Header.Set("Access-Control-Request-Method", http.MethodGet)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, allowed)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://app.afterword.io" {
		t.Errorf("Access-Control-Allow-Origin = %q, want the configured origin", got)
	}

	denied := httptest.NewRequest(http.MethodOptions, "/healthz", nil)
	denied.Header.Set("Origin", "https://evil.example")
	denied.Header.Set("Access-Control-Request-Method", http.MethodGet)
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, denied)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want empty for an unknown origin", got)
	}
}

func TestRouterRecoversFromPanics(t *testing.T) {
	router := NewRouter(RouterOptions{
		Logger:         zerolog.New(io.Discard),
		DB:             &stubPinger{},
		AllowedOrigin:  "https://app.afterword.io",
		RequestTimeout: time.Second,
		Mount: func(r chi.Router) {
			r.Get("/boom", func(http.ResponseWriter, *http.Request) {
				panic("boom")
			})
		},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", recorder.Code)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"internal_error"`) {
		t.Errorf("body = %s", got)
	}
	if strings.Contains(recorder.Body.String(), "boom") {
		t.Error("response leaks the panic value")
	}
}

func TestRouterTimesOutSlowHandlers(t *testing.T) {
	router := NewRouter(RouterOptions{
		Logger:         zerolog.New(io.Discard),
		DB:             &stubPinger{},
		AllowedOrigin:  "https://app.afterword.io",
		RequestTimeout: 20 * time.Millisecond,
		Mount: func(r chi.Router) {
			r.Get("/slow", func(_ http.ResponseWriter, req *http.Request) {
				<-req.Context().Done()
			})
		},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
	if got := recorder.Body.String(); !strings.Contains(got, `"request_timeout"`) {
		t.Errorf("body = %s", got)
	}
}
