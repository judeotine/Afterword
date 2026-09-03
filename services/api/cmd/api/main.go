package main

import (
	"context"
	"errors"
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
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
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

	library, err := buildLibrary(cfg, pool, logger)
	if err != nil {
		return err
	}

	apiServer, err := buildAPI(cfg, pool, library, logger)
	if err != nil {
		return err
	}

	if library != nil {
		runner, runnerErr := newPurgeRunner(cfg, pool, logger)
		if runnerErr != nil {
			return runnerErr
		}
		go func() {
			if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error().Err(err).Msg("job runner stopped")
			}
		}()
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

func buildLibrary(cfg config.Config, pool *db.Pool, logger zerolog.Logger) (*meetings.Service, error) {
	if !cfg.StorageConfigured() {
		logger.Warn().Msg("object storage is disabled: set S3_ACCESS_KEY and S3_SECRET_KEY to enable meetings, transcripts and sharing")
		return nil, nil
	}

	client, err := newStorageClient(cfg)
	if err != nil {
		return nil, err
	}
	return meetings.NewService(meetings.ServiceOptions{
		Pool:               pool.Pool(),
		Storage:            client,
		Buckets:            storageBuckets(cfg),
		Jobs:               jobs.NewQueue(pool.Pool()),
		Logger:             logger,
		UploadTTL:          cfg.S3.UploadTTL,
		DownloadTTL:        cfg.S3.DownloadTTL,
		MaxAudioBytes:      cfg.S3.MaxAudioBytes,
		MaxTranscriptBytes: cfg.S3.MaxTranscriptBytes,
	})
}

func newStorageClient(cfg config.Config) (*storage.S3Client, error) {
	return storage.NewS3Client(storage.Options{
		Endpoint:     cfg.S3.Endpoint,
		Region:       cfg.S3.Region,
		AccessKey:    cfg.S3.AccessKey,
		SecretKey:    cfg.S3.SecretKey,
		UsePathStyle: cfg.S3.UsePathStyle,
	})
}

func storageBuckets(cfg config.Config) storage.Buckets {
	return storage.Buckets{
		Audio:       cfg.S3.AudioBucket,
		Transcripts: cfg.S3.TranscriptsBucket,
		Clips:       cfg.S3.ClipsBucket,
		Exports:     cfg.S3.ExportsBucket,
	}.WithDefaults()
}

func newPurgeRunner(cfg config.Config, pool *db.Pool, logger zerolog.Logger) (*jobs.Runner, error) {
	client, err := newStorageClient(cfg)
	if err != nil {
		return nil, err
	}
	runner := jobs.NewRunner(jobs.NewQueue(pool.Pool()), jobs.WithLogger(logger))
	if err := runner.Register(meetings.KindPurge, 1, meetings.NewPurgeHandler(client)); err != nil {
		return nil, err
	}
	return runner, nil
}

func buildAPI(cfg config.Config, pool *db.Pool, library *meetings.Service, logger zerolog.Logger) (*api.Server, error) {
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
		Meetings:   library,
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
