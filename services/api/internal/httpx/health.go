package httpx

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/hlog"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type SchemaReporter interface {
	SchemaVersion(ctx context.Context) (int64, bool, error)
}

type poolProvider interface {
	Pool() *pgxpool.Pool
}

var ExpectedSchemaVersion string

const schemaVersionStatement = `SELECT version, dirty FROM schema_migrations ORDER BY version DESC LIMIT 1`

const (
	statusOK       = "ok"
	statusDegraded = "degraded"

	databaseUp      = "up"
	databaseDown    = "down"
	databaseUnknown = "unknown"

	migrationsOK      = "ok"
	migrationsAhead   = "ahead"
	migrationsPending = "pending"
	migrationsDirty   = "dirty"
	migrationsUnknown = "unknown"
)

type healthResponse struct {
	Status     string `json:"status"`
	DB         string `json:"db"`
	Migrations string `json:"migrations"`
}

func Health(pinger Pinger, timeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if pinger == nil {
			WriteJSON(w, r, http.StatusServiceUnavailable, healthResponse{
				Status:     statusDegraded,
				DB:         databaseUnknown,
				Migrations: migrationsUnknown,
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		if err := pinger.Ping(ctx); err != nil {
			hlog.FromRequest(r).Warn().Err(err).Msg("health check database ping failed")
			WriteJSON(w, r, http.StatusServiceUnavailable, healthResponse{
				Status:     statusDegraded,
				DB:         databaseDown,
				Migrations: migrationsUnknown,
			})
			return
		}

		migrations, err := schemaState(ctx, pinger)
		if err != nil {
			hlog.FromRequest(r).Warn().Err(err).Msg("health check schema version read failed")
		}
		if migrations != migrationsOK && migrations != migrationsAhead {
			WriteJSON(w, r, http.StatusServiceUnavailable, healthResponse{
				Status:     statusDegraded,
				DB:         databaseUp,
				Migrations: migrations,
			})
			return
		}

		WriteJSON(w, r, http.StatusOK, healthResponse{
			Status:     statusOK,
			DB:         databaseUp,
			Migrations: migrations,
		})
	}
}

func schemaState(ctx context.Context, pinger Pinger) (string, error) {
	reporter, ok := schemaReporterFor(pinger)
	if !ok {
		return migrationsUnknown, nil
	}

	version, dirty, err := reporter.SchemaVersion(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return migrationsPending, nil
		}
		return migrationsUnknown, err
	}
	if dirty {
		return migrationsDirty, nil
	}

	expected, known := expectedSchemaVersion()
	switch {
	case !known:
		return migrationsOK, nil
	case version < expected:
		return migrationsPending, nil
	case version > expected:
		return migrationsAhead, nil
	default:
		return migrationsOK, nil
	}
}

func schemaReporterFor(pinger Pinger) (SchemaReporter, bool) {
	if reporter, ok := pinger.(SchemaReporter); ok {
		return reporter, true
	}
	if provider, ok := pinger.(poolProvider); ok {
		return poolSchemaReporter{pool: provider.Pool()}, true
	}
	return nil, false
}

type poolSchemaReporter struct {
	pool *pgxpool.Pool
}

func (p poolSchemaReporter) SchemaVersion(ctx context.Context) (int64, bool, error) {
	if p.pool == nil {
		return 0, false, errors.New("no connection pool")
	}
	var version int64
	var dirty bool
	if err := p.pool.QueryRow(ctx, schemaVersionStatement).Scan(&version, &dirty); err != nil {
		return 0, false, err
	}
	return version, dirty, nil
}

func expectedSchemaVersion() (int64, bool) {
	value := strings.TrimSpace(ExpectedSchemaVersion)
	if value == "" {
		return 0, false
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, false
	}
	return parsed, true
}
