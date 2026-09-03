package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
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

	apiServer, err := buildAPI(cfg, pool, logger)
	if err != nil {
		return err
	}

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:         logger,
		DB:             pool,
		Metrics:        httpx.NewMetrics(),
		AllowedOrigin:  cfg.AppBaseURL,
		TrustedProxies: cfg.TrustedProxies,
		RequestTimeout: cfg.RequestTimeout,
		Mount:          apiServer.Routes,
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

func buildAPI(cfg config.Config, pool *db.Pool, logger zerolog.Logger) (*api.Server, error) {
	authStore, err := auth.NewStore(pool.Pool())
	if err != nil {
		return nil, err
	}

	accountsService, err := accounts.NewService(pool.Pool())
	if err != nil {
		return nil, err
	}

	tokens, err := auth.NewTokenIssuer([]byte(cfg.JWTSecret))
	if err != nil {
		return nil, err
	}

	refresh, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: authStore})
	if err != nil {
		return nil, err
	}

	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{
		Issuer:      tokens,
		Memberships: accountsService,
	})
	if err != nil {
		return nil, err
	}

	emailSender, err := buildEmailSender(cfg, logger)
	if err != nil {
		return nil, err
	}

	otp, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       authStore,
		EmailSender: emailSender,
		SMSSender:   buildSMSSender(cfg, logger),
	})
	if err != nil {
		return nil, err
	}

	var google *auth.GoogleAuthenticator
	if cfg.GoogleConfigured() {
		google, err = auth.NewGoogleAuthenticator(auth.GoogleOptions{
			ClientID:     cfg.Auth.GoogleClientID,
			ClientSecret: cfg.Auth.GoogleClientSecret,
			RedirectURL:  cfg.Auth.GoogleRedirectURL,
			Store:        authStore,
		})
		if err != nil {
			return nil, err
		}
	} else {
		logger.Warn().Msg("google sign-in is disabled: set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET to enable it")
	}

	return api.NewServer(api.ServerOptions{
		Accounts:   accountsService,
		OTP:        otp,
		Tokens:     tokens,
		Refresh:    refresh,
		Middleware: middleware,
		Google:     google,
		Email:      emailSender,
		AppBaseURL: cfg.AppBaseURL,
	})
}

func buildEmailSender(cfg config.Config, logger zerolog.Logger) (auth.EmailSender, error) {
	if cfg.Auth.EmailSender != "smtp" {
		logger.Warn().Msg("email sender is the development log sender: verification codes are written to the log")
		return auth.NewLogEmailSender(logger), nil
	}
	return auth.NewSMTPEmailSender(auth.SMTPOptions{
		Host:     cfg.Auth.SMTP.Host,
		Port:     cfg.Auth.SMTP.Port,
		Username: cfg.Auth.SMTP.Username,
		Password: cfg.Auth.SMTP.Password,
		From:     cfg.Auth.SMTP.From,
		StartTLS: cfg.Auth.SMTP.StartTLS,
	})
}

func buildSMSSender(cfg config.Config, logger zerolog.Logger) auth.SMSSender {
	if cfg.Auth.SMSSender != "log" {
		return auth.NewNoopSMSSender()
	}
	logger.Warn().Msg("sms sender is the development log sender: verification codes are written to the log")
	return auth.NewLogSMSSender(logger)
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
