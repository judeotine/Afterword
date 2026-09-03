//go:build integration

package db_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/rs/zerolog"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/judeotine/afterword/services/api/internal/db"
)

const containerImage = "pgvector/pgvector:pg16"

func TestPoolPingTracksMigrationState(t *testing.T) {
	databaseURL := postgresURL(t)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)

	pool, err := db.Connect(ctx, db.Options{URL: databaseURL, MaxConns: 4, ConnectTimeout: 5 * time.Second}, logger)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	migrator := newMigrator(t, databaseURL)

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping succeeded before migrations were applied")
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping after migrate up: %v", err)
	}

	version, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("migrate version: %v", err)
	}
	if version != 1 || dirty {
		t.Fatalf("schema version = %d dirty = %t, want 1 and clean", version, dirty)
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

func postgresURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv("DATABASE_URL"); url != "" {
		return url
	}
	if !dockerAvailable() {
		t.Skip("no DATABASE_URL and no reachable Docker daemon: set DATABASE_URL or start Docker to run integration tests")
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

	url, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("container connection string: %v", err)
	}
	return url
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
