package dbtest

import (
	"context"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	IntegrationURLEnv = "INTEGRATION_DATABASE_URL"
	FallbackURLEnv    = "DATABASE_URL"
	ContainerImage    = "pgvector/pgvector:pg16"

	SkipReason = "no " + IntegrationURLEnv + ", no " + FallbackURLEnv +
		" and no reachable Docker daemon: set one of them or start Docker to run integration tests"
)

var (
	baseOnce sync.Once
	baseURL  string
	baseErr  error
)

func BaseURL(t *testing.T) string {
	t.Helper()
	baseOnce.Do(resolveBaseURL)
	if baseErr != nil {
		t.Fatalf("resolve base database url: %v", baseErr)
	}
	if baseURL == "" {
		t.Skip(SkipReason)
	}
	return baseURL
}

func resolveBaseURL() {
	for _, key := range []string{IntegrationURLEnv, FallbackURLEnv} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			baseURL = value
			return
		}
	}
	if !dockerAvailable() {
		return
	}

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx, ContainerImage,
		tcpostgres.WithDatabase("afterword"),
		tcpostgres.WithUsername("afterword"),
		tcpostgres.WithPassword("afterword"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(2*time.Minute),
		),
	)
	if err != nil {
		baseErr = fmt.Errorf("start postgres container: %w", err)
		return
	}
	connString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		baseErr = fmt.Errorf("container connection string: %w", err)
		return
	}
	baseURL = connString
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

func NewDatabaseURL(t *testing.T) string {
	t.Helper()
	base := BaseURL(t)
	name := databaseName()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect to base database: %v", err)
	}
	defer func() {
		_ = admin.Close(context.Background())
	}()

	if _, err := admin.Exec(ctx, `CREATE DATABASE `+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		dropCtx, dropCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer dropCancel()
		cleaner, err := pgx.Connect(dropCtx, base)
		if err != nil {
			t.Logf("connect to base database for cleanup: %v", err)
			return
		}
		defer func() {
			_ = cleaner.Close(context.Background())
		}()
		if _, err := cleaner.Exec(dropCtx, `DROP DATABASE IF EXISTS `+pgx.Identifier{name}.Sanitize()+` WITH (FORCE)`); err != nil {
			t.Logf("drop test database %s: %v", name, err)
		}
	})

	return replaceDatabaseName(t, base, name)
}

func New(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := NewDatabaseURL(t)
	Migrate(t, databaseURL)
	return Open(t, databaseURL)
}

func Open(t *testing.T, databaseURL string) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database url: %v", err)
	}
	config.MaxConns = 20

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func Migrate(t *testing.T, databaseURL string) {
	t.Helper()
	migrator := NewMigrator(t, databaseURL)
	if err := migrator.Up(); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
}

func NewMigrator(t *testing.T, databaseURL string) *migrate.Migrate {
	t.Helper()
	migrator, err := migrate.New("file://"+MigrationsDir(t), databaseURL)
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

func MigrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate dbtest source file")
	}
	dir, err := filepath.Abs(filepath.Join(filepath.Dir(file), "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations directory: %v", err)
	}
	return dir
}

func NewWorkspace(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	slug := "ws-" + strings.ToLower(uuid.NewString())
	var id uuid.UUID
	err := pool.QueryRow(ctx,
		`INSERT INTO workspaces (name, slug) VALUES ($1, $2) RETURNING id`,
		"Test Workspace", slug,
	).Scan(&id)
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	return id
}

func databaseName() string {
	return fmt.Sprintf("afterword_test_%d_%d", time.Now().UnixNano(), rand.Int63n(1_000_000))
}

func replaceDatabaseName(t *testing.T, base string, name string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base database url: %v", err)
	}
	parsed.Path = "/" + name
	return parsed.String()
}
