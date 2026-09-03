//go:build integration

package db_test

import (
	"context"
	"crypto/rand"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/judeotine/afterword/services/api/internal/db"
)

const (
	containerImage           = "pgvector/pgvector:pg16"
	integrationURLEnvVar     = "INTEGRATION_DATABASE_URL"
	scratchDatabasePrefix    = "afterword_it_"
	scratchDatabaseNameChars = 12
)

func TestPoolPingTracksMigrationState(t *testing.T) {
	databaseURL := scratchDatabaseURL(t)
	ctx := context.Background()

	pool, err := db.Connect(ctx, db.Options{URL: databaseURL, MaxConns: 4, ConnectTimeout: 5 * time.Second}, zerolog.New(io.Discard))
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	migrator := newMigrator(t, databaseURL)

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping succeeded on a database with no schema")
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping after migrate up: %v", err)
	}

	appliedVersion, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("migrate version: %v", err)
	}
	if dirty {
		t.Fatalf("schema version %d is dirty after migrate up", appliedVersion)
	}
	if appliedVersion < 1 {
		t.Fatalf("schema version = %d, want at least 1", appliedVersion)
	}

	if err := migrator.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping succeeded after the schema was rolled back")
	}
}

func newMigrator(t *testing.T, databaseURL string) *migrate.Migrate {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations directory: %v", err)
	}
	migrator, err := migrate.New("file://"+dir, databaseURL)
	if err != nil {
		t.Fatalf("open migrator: %v", err)
	}
	t.Cleanup(func() {
		sourceErr, databaseErr := migrator.Close()
		if sourceErr != nil {
			t.Errorf("close migration source: %v", sourceErr)
		}
		if databaseErr != nil {
			t.Errorf("close migration database: %v", databaseErr)
		}
	})
	return migrator
}

func scratchDatabaseURL(t *testing.T) string {
	t.Helper()
	adminURL, ok := os.LookupEnv(integrationURLEnvVar)
	if !ok || strings.TrimSpace(adminURL) == "" {
		adminURL = containerDatabaseURL(t)
	}
	return createScratchDatabase(t, adminURL)
}

func createScratchDatabase(t *testing.T, adminURL string) string {
	t.Helper()
	ctx := context.Background()
	name := scratchDatabaseName()
	quoted := pgx.Identifier{name}.Sanitize()

	admin := connectAdmin(ctx, t, adminURL)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+quoted); err != nil {
		_ = admin.Close(ctx)
		t.Fatalf("create scratch database %s: %v", name, err)
	}
	if err := admin.Close(ctx); err != nil {
		t.Errorf("close admin connection: %v", err)
	}

	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		dropper := connectAdmin(cleanupCtx, t, adminURL)
		defer func() {
			if err := dropper.Close(context.Background()); err != nil {
				t.Errorf("close admin connection: %v", err)
			}
		}()
		if _, err := dropper.Exec(cleanupCtx, "DROP DATABASE IF EXISTS "+quoted+" WITH (FORCE)"); err != nil {
			t.Errorf("drop scratch database %s: %v", name, err)
		}
	})

	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse %s: %v", integrationURLEnvVar, err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}

func connectAdmin(ctx context.Context, t *testing.T, adminURL string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect to the maintenance database: %v", err)
	}
	return conn
}

func scratchDatabaseName() string {
	return scratchDatabasePrefix + strings.ToLower(rand.Text()[:scratchDatabaseNameChars])
}

func containerDatabaseURL(t *testing.T) string {
	t.Helper()
	if !dockerAvailable() {
		t.Skipf("set %s to a disposable Postgres server or start Docker to run integration tests", integrationURLEnvVar)
	}

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, containerImage,
		tcpostgres.WithDatabase("afterword"),
		tcpostgres.WithUsername("afterword"),
		tcpostgres.WithPassword("afterword"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	return connectionString
}

func dockerAvailable() bool {
	provider, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer func() {
		_ = provider.Close()
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return provider.Health(ctx) == nil
}
