//go:build integration

package db_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/db"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

func TestPoolPingTracksMigrationState(t *testing.T) {
	databaseURL := dbtest.NewDatabaseURL(t)
	ctx := context.Background()
	logger := zerolog.New(io.Discard)

	pool, err := db.Connect(ctx, db.Options{URL: databaseURL, MaxConns: 4, ConnectTimeout: 5 * time.Second}, logger)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(pool.Close)

	migrator := dbtest.NewMigrator(t, databaseURL)

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping succeeded before migrations were applied")
	}

	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		t.Fatalf("Ping after migrate up: %v", err)
	}

	latest := latestMigrationVersion(t)
	version, dirty, err := migrator.Version()
	if err != nil {
		t.Fatalf("migrate version: %v", err)
	}
	if version != latest || dirty {
		t.Fatalf("schema version = %d dirty = %t, want %d and clean", version, dirty, latest)
	}

	if err := migrator.Down(); err != nil {
		t.Fatalf("migrate down: %v", err)
	}

	if err := pool.Ping(ctx); err == nil {
		t.Fatal("Ping succeeded after the schema was rolled back")
	}

	if remaining := publicTableCount(ctx, t, databaseURL); remaining != 0 {
		t.Fatalf("%d tables survived the down migrations, want 0", remaining)
	}
}

func latestMigrationVersion(t *testing.T) uint {
	t.Helper()
	entries, err := os.ReadDir(dbtest.MigrationsDir(t))
	if err != nil {
		t.Fatalf("read migrations directory: %v", err)
	}
	var latest uint
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		digits := name[:strings.IndexByte(name, '_')]
		var version uint
		for _, char := range digits {
			if char < '0' || char > '9' {
				t.Fatalf("migration %s does not start with a numeric version", name)
			}
			version = version*10 + uint(char-'0')
		}
		if version > latest {
			latest = version
		}
	}
	if latest == 0 {
		t.Fatalf("no up migrations found in %s", filepath.Clean(dbtest.MigrationsDir(t)))
	}
	return latest
}

func publicTableCount(ctx context.Context, t *testing.T, databaseURL string) int {
	t.Helper()
	pool := dbtest.Open(t, databaseURL)
	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public' AND table_name <> 'schema_migrations'`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("count public tables: %v", err)
	}
	return count
}
