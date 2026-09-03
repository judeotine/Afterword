package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	databaseURLEnv       = "DATABASE_URL"
	migrationsDirEnv     = "MIGRATIONS_DIR"
	defaultMigrationsDir = "/migrations"
	usage                = "usage: migrate up | migrate down [steps|all] | migrate force <version> | migrate version"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "afterword-migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	databaseURL := strings.TrimSpace(os.Getenv(databaseURLEnv))
	if databaseURL == "" {
		return errors.New(databaseURLEnv + " is required")
	}

	directory := strings.TrimSpace(os.Getenv(migrationsDirEnv))
	if directory == "" {
		directory = defaultMigrationsDir
	}

	migrator, err := migrate.New("file://"+directory, databaseURL)
	if err != nil {
		return fmt.Errorf("open migrator: %w", err)
	}
	defer closeMigrator(migrator)

	switch args[0] {
	case "up":
		return up(migrator, args[1:])
	case "down":
		return down(migrator, args[1:])
	case "force":
		return force(migrator, args[1:])
	case "version":
		return printVersion(migrator)
	default:
		return fmt.Errorf("unknown command %q, %s", args[0], usage)
	}
}

func up(migrator *migrate.Migrate, rest []string) error {
	if len(rest) != 0 {
		return errors.New(usage)
	}
	if err := migrator.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			report(migrator, "already up to date")
			return nil
		}
		return fmt.Errorf("migrate up: %w", err)
	}
	report(migrator, "migrated up")
	return nil
}

func down(migrator *migrate.Migrate, rest []string) error {
	if len(rest) == 1 && rest[0] == "all" {
		return downAll(migrator)
	}
	steps, err := downSteps(rest)
	if err != nil {
		return err
	}
	if err := migrator.Steps(-steps); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			report(migrator, "nothing to roll back")
			return nil
		}
		return fmt.Errorf("migrate down %d: %w", steps, err)
	}
	report(migrator, fmt.Sprintf("rolled back %d", steps))
	return nil
}

func downAll(migrator *migrate.Migrate) error {
	if err := migrator.Down(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			report(migrator, "nothing to roll back")
			return nil
		}
		return fmt.Errorf("migrate down all: %w", err)
	}
	report(migrator, "rolled back every migration")
	return nil
}

func downSteps(rest []string) (int, error) {
	if len(rest) == 0 {
		return 1, nil
	}
	if len(rest) > 1 {
		return 0, errors.New(usage)
	}
	steps, err := strconv.Atoi(rest[0])
	if err != nil || steps < 1 {
		return 0, errors.New("down steps must be a positive whole number")
	}
	return steps, nil
}

func force(migrator *migrate.Migrate, rest []string) error {
	if len(rest) != 1 {
		return errors.New(usage)
	}
	version, err := strconv.Atoi(rest[0])
	if err != nil || version < 0 {
		return errors.New("force version must be a whole number, zero or greater")
	}
	if err := migrator.Force(version); err != nil {
		return fmt.Errorf("force version %d: %w", version, err)
	}
	report(migrator, fmt.Sprintf("forced to version %d", version))
	return nil
}

func printVersion(migrator *migrate.Migrate) error {
	version, dirty, err := migrator.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		fmt.Println("afterword-migrate: no migrations applied")
		return nil
	}
	if err != nil {
		return fmt.Errorf("read version: %w", err)
	}
	fmt.Printf("afterword-migrate: version=%d dirty=%t\n", version, dirty)
	return nil
}

func report(migrator *migrate.Migrate, action string) {
	version, dirty, err := migrator.Version()
	if err != nil {
		fmt.Printf("afterword-migrate: %s\n", action)
		return
	}
	fmt.Printf("afterword-migrate: %s, version=%d dirty=%t\n", action, version, dirty)
}

func closeMigrator(migrator *migrate.Migrate) {
	sourceErr, databaseErr := migrator.Close()
	if sourceErr != nil {
		fmt.Fprintf(os.Stderr, "afterword-migrate: close source: %v\n", sourceErr)
	}
	if databaseErr != nil {
		fmt.Fprintf(os.Stderr, "afterword-migrate: close database: %v\n", databaseErr)
	}
}
