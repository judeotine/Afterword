package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type stubPinger struct {
	err           error
	delay         time.Duration
	calls         int
	lastCtx       context.Context
	schemaVersion int64
	schemaDirty   bool
	schemaErr     error
	schemaCalls   int
}

func (s *stubPinger) Ping(ctx context.Context) error {
	s.calls++
	s.lastCtx = ctx
	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.err
}

func (s *stubPinger) SchemaVersion(_ context.Context) (int64, bool, error) {
	s.schemaCalls++
	return s.schemaVersion, s.schemaDirty, s.schemaErr
}

type pingOnly struct{}

func (pingOnly) Ping(_ context.Context) error {
	return nil
}

func withExpectedSchemaVersion(t *testing.T, value string) {
	t.Helper()
	previous := ExpectedSchemaVersion
	ExpectedSchemaVersion = value
	t.Cleanup(func() {
		ExpectedSchemaVersion = previous
	})
}

func serveHealth(t *testing.T, pinger Pinger) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	Health(pinger, time.Second).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	return recorder
}

func assertHealth(t *testing.T, recorder *httptest.ResponseRecorder, code int, migrations string) {
	t.Helper()
	if recorder.Code != code {
		t.Errorf("status = %d, want %d", recorder.Code, code)
	}
	body := decodeHealth(t, recorder)
	if body.Migrations != migrations {
		t.Errorf("migrations = %q, want %q", body.Migrations, migrations)
	}
}

func decodeHealth(t *testing.T, recorder *httptest.ResponseRecorder) healthResponse {
	t.Helper()
	var body healthResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not JSON: %v (%s)", err, recorder.Body.String())
	}
	return body
}

func TestHealthReportsUpWhenDatabaseAnswers(t *testing.T) {
	pinger := &stubPinger{}
	recorder := httptest.NewRecorder()

	Health(pinger, time.Second).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	body := decodeHealth(t, recorder)
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.DB != "up" {
		t.Errorf("db = %q, want up", body.DB)
	}
	if pinger.calls != 1 {
		t.Errorf("pinger called %d times, want 1", pinger.calls)
	}
}

func TestHealthReportsDownWhenDatabaseFails(t *testing.T) {
	pinger := &stubPinger{err: errors.New("connection refused")}
	recorder := httptest.NewRecorder()

	Health(pinger, time.Second).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	body := decodeHealth(t, recorder)
	if body.Status != "degraded" {
		t.Errorf("status = %q, want degraded", body.Status)
	}
	if body.DB != "down" {
		t.Errorf("db = %q, want down", body.DB)
	}
}

func TestHealthDoesNotLeakDatabaseErrorDetail(t *testing.T) {
	pinger := &stubPinger{err: errors.New("password authentication failed for user afterword")}
	recorder := httptest.NewRecorder()

	Health(pinger, time.Second).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if body := recorder.Body.String(); strings.Contains(body, "password") {
		t.Errorf("response leaks driver detail: %s", body)
	}
}

func TestHealthBoundsThePingWithATimeout(t *testing.T) {
	pinger := &stubPinger{delay: time.Second}
	recorder := httptest.NewRecorder()

	start := time.Now()
	Health(pinger, 20*time.Millisecond).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("handler took %s, want the ping to be cut short", elapsed)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
}

func TestHealthWithoutDatabaseReportsUnknown(t *testing.T) {
	recorder := httptest.NewRecorder()

	Health(nil, time.Second).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	body := decodeHealth(t, recorder)
	if body.DB != "unknown" {
		t.Errorf("db = %q, want unknown", body.DB)
	}
}

func TestHealthReportsMigrationsOKWhenTheSchemaIsClean(t *testing.T) {
	recorder := serveHealth(t, &stubPinger{schemaVersion: 9})

	assertHealth(t, recorder, http.StatusOK, "ok")
	if body := decodeHealth(t, recorder); body.Status != "ok" || body.DB != "up" {
		t.Errorf("body = %+v, want ok/up", body)
	}
}

func TestHealthReportsPendingWhenNoMigrationHasRun(t *testing.T) {
	pinger := &stubPinger{schemaErr: pgx.ErrNoRows}

	assertHealth(t, serveHealth(t, pinger), http.StatusServiceUnavailable, "pending")
	if pinger.schemaCalls != 1 {
		t.Errorf("schema read called %d times, want 1", pinger.schemaCalls)
	}
}

func TestHealthReportsDirtyWhenAMigrationFailedHalfway(t *testing.T) {
	assertHealth(t, serveHealth(t, &stubPinger{schemaVersion: 7, schemaDirty: true}), http.StatusServiceUnavailable, "dirty")
}

func TestHealthReportsUnknownWhenTheSchemaCannotBeRead(t *testing.T) {
	recorder := serveHealth(t, &stubPinger{schemaErr: errors.New("relation does not exist")})

	assertHealth(t, recorder, http.StatusServiceUnavailable, "unknown")
	if body := recorder.Body.String(); strings.Contains(body, "relation") {
		t.Errorf("response leaks driver detail: %s", body)
	}
}

func TestHealthReportsUnknownWhenTheDriverCannotReportASchema(t *testing.T) {
	assertHealth(t, serveHealth(t, pingOnly{}), http.StatusServiceUnavailable, "unknown")
}

func TestHealthReportsPendingWhenTheSchemaIsBehindTheImage(t *testing.T) {
	withExpectedSchemaVersion(t, "9")

	assertHealth(t, serveHealth(t, &stubPinger{schemaVersion: 8}), http.StatusServiceUnavailable, "pending")
}

func TestHealthReportsOKWhenTheSchemaMatchesTheImage(t *testing.T) {
	withExpectedSchemaVersion(t, "9")

	assertHealth(t, serveHealth(t, &stubPinger{schemaVersion: 9}), http.StatusOK, "ok")
}

func TestHealthStaysUpWhenTheSchemaIsAheadOfTheImage(t *testing.T) {
	withExpectedSchemaVersion(t, "9")

	assertHealth(t, serveHealth(t, &stubPinger{schemaVersion: 10}), http.StatusOK, "ahead")
}

func TestHealthIgnoresAnUnparsableExpectedSchemaVersion(t *testing.T) {
	withExpectedSchemaVersion(t, "not-a-number")

	assertHealth(t, serveHealth(t, &stubPinger{schemaVersion: 3}), http.StatusOK, "ok")
}

func TestHealthReportsMigrationsUnknownWhenTheDatabaseIsDown(t *testing.T) {
	assertHealth(t, serveHealth(t, &stubPinger{err: errors.New("down")}), http.StatusServiceUnavailable, "unknown")
}
