package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/config"
	"github.com/judeotine/afterword/services/api/internal/db"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/version"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "afterword-api: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.FromEnvironment()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	build := version.Current()
	logger.Info().
		Str("version", build.Version).
		Str("commit", build.Commit).
		Str("build_date", build.BuildDate).
		Str("address", cfg.Address()).
		Msg("starting afterword api")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancelConnect := context.WithTimeout(ctx, 10*time.Second)
	defer cancelConnect()

	pool, err := db.Connect(connectCtx, db.Options{
		URL:             cfg.DatabaseURL,
		MaxConns:        cfg.DatabaseMaxConns,
		MinConns:        1,
		MaxConnLifetime: time.Hour,
		MaxConnIdleTime: 30 * time.Minute,
		ConnectTimeout:  5 * time.Second,
	}, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(connectCtx); err != nil {
		logger.Warn().Err(err).Msg("database not reachable at startup, serving in degraded mode")
	}

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:         logger,
		DB:             pool,
		Metrics:        httpx.NewMetrics(),
		AllowedOrigin:  cfg.AppBaseURL,
		TrustedProxies: cfg.TrustedProxies,
		RequestTimeout: cfg.RequestTimeout,
	})

	server := httpx.NewServer(httpx.ServerOptions{
		Address: cfg.Address(),
		Handler: router,
	})

	logger.Info().Str("address", cfg.Address()).Msg("listening")
	if err := httpx.Serve(ctx, server, cfg.ShutdownTimeout); err != nil {
		return err
	}
	logger.Info().Msg("shutdown complete")
	return nil
}

func newLogger(level string) zerolog.Logger {
	parsed, err := zerolog.ParseLevel(level)
	if err != nil {
		parsed = zerolog.InfoLevel
	}
	zerolog.TimeFieldFormat = time.RFC3339Nano
	return zerolog.New(os.Stdout).
		Level(parsed).
		With().
		Timestamp().
		Str("service", "afterword-api").
		Logger()
}
