package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/botjobs"
	"github.com/judeotine/afterword/services/api/internal/config"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/db"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/payments"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/version"
)

const (
	taskMonthlyCreditGrant             = "monthly-credit-grant"
	taskPendingPaymentReaper           = "pending-payment-reaper"
	lockKeyPendingPaymentReaper  int64 = 0x616677726561706d
	pendingPaymentReaperInterval       = time.Hour
	backgroundStopGrace                = 30 * time.Second
	fakeCheckoutPath                   = "/billing/fake"
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

	billingCfg, err := config.BillingFromEnvironment()
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

	components, err := buildAPI(cfg, pool, library, logger)
	if err != nil {
		return err
	}

	billingService, err := buildBilling(cfg, billingCfg, pool, logger)
	if err != nil {
		return err
	}

	billingServer, err := api.NewBillingServer(api.BillingOptions{
		Billing:    billingService,
		Middleware: components.middleware,
		AdminToken: billingCfg.AdminToken,
		AppBaseURL: cfg.AppBaseURL,
	})
	if err != nil {
		return err
	}
	if !billingCfg.AdminConfigured() {
		logger.Warn().Msg("credit adjustments are disabled: set ADMIN_TOKEN to enable them")
	}

	botJobServer, err := buildBotJobs(cfg, pool, components, logger)
	if err != nil {
		return err
	}

	var background sync.WaitGroup

	if library != nil {
		runner, runnerErr := newPurgeRunner(cfg, pool, logger)
		if runnerErr != nil {
			return runnerErr
		}
		background.Add(1)
		go func() {
			defer background.Done()
			if err := runner.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error().Err(err).Msg("job runner stopped")
			}
		}()
	}

	scheduler, err := buildScheduler(billingCfg, pool, components, library, billingService, logger)
	if err != nil {
		return err
	}
	if scheduler != nil {
		background.Add(1)
		go func() {
			defer background.Done()
			if err := scheduler.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error().Err(err).Msg("scheduler stopped")
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
		Mount: func(router chi.Router) {
			components.server.Routes(router)
			billingServer.Routes(router)
			botJobServer.Routes(router)
		},
	})

	server := httpx.NewServer(httpx.ServerOptions{
		Address: cfg.Address(),
		Handler: router,
	})

	logger.Info().Str("address", cfg.Address()).Msg("listening")
	serveErr := httpx.Serve(ctx, server, cfg.ShutdownTimeout)
	awaitBackground(&background, backgroundStopGrace, logger)
	if serveErr != nil {
		return serveErr
	}
	logger.Info().Msg("shutdown complete")
	return nil
}

func awaitBackground(background *sync.WaitGroup, grace time.Duration, logger zerolog.Logger) {
	stopped := make(chan struct{})
	go func() {
		background.Wait()
		close(stopped)
	}()

	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-stopped:
	case <-timer.C:
		logger.Warn().Dur("grace", grace).Msg("background tasks did not stop before the grace period ended")
	}
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
		Credits:            credits.NewLedger(pool.Pool()),
		Logger:             logger,
		UploadTTL:          cfg.S3.UploadTTL,
		DownloadTTL:        cfg.S3.DownloadTTL,
		MaxAudioBytes:      cfg.S3.MaxAudioBytes,
		MaxTranscriptBytes: cfg.S3.MaxTranscriptBytes,
		ShareRateWindow:    cfg.ShareRateWindow,
		ShareRateLimit:     cfg.ShareRateLimit,
		AbandonedUploadTTL: cfg.AbandonedUploadTTL,
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

type apiComponents struct {
	server     *api.Server
	middleware *auth.Middleware
	authStore  *auth.Store
}

func buildAPI(cfg config.Config, pool *db.Pool, library *meetings.Service, logger zerolog.Logger) (apiComponents, error) {
	authStore, err := auth.NewStore(pool.Pool())
	if err != nil {
		return apiComponents{}, err
	}

	accountsService, err := accounts.NewService(pool.Pool())
	if err != nil {
		return apiComponents{}, err
	}

	tokens, err := auth.NewTokenIssuer([]byte(cfg.JWTSecret))
	if err != nil {
		return apiComponents{}, err
	}

	refresh, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: authStore})
	if err != nil {
		return apiComponents{}, err
	}

	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{
		Issuer:      tokens,
		Memberships: accountsService,
	})
	if err != nil {
		return apiComponents{}, err
	}

	emailSender, err := buildEmailSender(cfg, logger)
	if err != nil {
		return apiComponents{}, err
	}

	otp, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       authStore,
		EmailSender: emailSender,
		SMSSender:   buildSMSSender(cfg, logger),
	})
	if err != nil {
		return apiComponents{}, err
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
			return apiComponents{}, err
		}
	} else {
		logger.Warn().Msg("google sign-in is disabled: set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET to enable it")
	}

	server, err := api.NewServer(api.ServerOptions{
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
	if err != nil {
		return apiComponents{}, err
	}
	return apiComponents{server: server, middleware: middleware, authStore: authStore}, nil
}

func buildBilling(cfg config.Config, billingCfg config.BillingConfig, pool *db.Pool, logger zerolog.Logger) (*billing.Service, error) {
	registry := payments.NewRegistry()

	if billingCfg.FakeConfigured() {
		fake, err := payments.NewFake(payments.FakeOptions{
			Secret:          billingCfg.FakeSecret,
			CheckoutBaseURL: cfg.AppBaseURL + fakeCheckoutPath,
		})
		if err != nil {
			return nil, err
		}
		if err := registry.Register(payments.FakeProviderName, fake); err != nil {
			return nil, err
		}
		logger.Warn().Msg("the fake payment provider is registered: never set ALLOW_FAKE_PAYMENTS in production")
	}

	if billingCfg.NylonPayConfigured() {
		nylonpay, err := payments.NewNylonPay(payments.NylonPayOptions{
			BaseURL:       billingCfg.NylonPay.BaseURL,
			APIKey:        billingCfg.NylonPay.APIKey,
			WebhookSecret: billingCfg.NylonPay.WebhookSecret,
		})
		if err != nil {
			return nil, err
		}
		if err := registry.Register(payments.NylonPayProviderName, nylonpay); err != nil {
			return nil, err
		}
	}

	defaultProvider := billingCfg.Provider
	if _, err := registry.Lookup(defaultProvider); err != nil {
		defaultProvider = ""
		logger.Warn().Str("provider", billingCfg.Provider).
			Msg("top-ups are disabled: set PAYMENT_PROVIDER and its credentials to enable them")
	}

	return billing.NewService(billing.ServiceOptions{
		Pool:             pool.Pool(),
		Providers:        registry,
		DefaultProvider:  defaultProvider,
		FreeGrantMinutes: billingCfg.FreeGrantMinutes,
		APIBaseURL:       cfg.APIBaseURL,
		CheckoutLimit:    billingCfg.CheckoutLimit,
		CheckoutWindow:   billingCfg.CheckoutWindow,
		Logger:           logger,
	})
}

func buildBotJobs(cfg config.Config, pool *db.Pool, components apiComponents, logger zerolog.Logger) (*api.BotJobServer, error) {
	service, err := botjobs.NewService(botjobs.ServiceOptions{Pool: pool.Pool()})
	if err != nil {
		return nil, err
	}
	entitlements, err := billing.NewEntitlements(credits.NewLedger(pool.Pool()), cfg.AppBaseURL)
	if err != nil {
		return nil, err
	}
	logger.Debug().Str("top_up_url", entitlements.TopUpURL()).Msg("credit entitlements ready")
	return api.NewBotJobServer(api.BotJobOptions{
		BotJobs:      service,
		Entitlements: entitlements,
		Middleware:   components.middleware,
	})
}

func startupTasks(billingCfg config.BillingConfig, authStore *auth.Store, library *meetings.Service, grantRun, reapRun func(context.Context) error) []jobs.ScheduledTask {
	tasks := make([]jobs.ScheduledTask, 0, 5)
	tasks = append(tasks, auth.MaintenanceTasks(authStore)...)
	tasks = append(tasks, meetings.MaintenanceTasks(library)...)

	if billingCfg.FreeGrantMinutes > 0 && grantRun != nil {
		tasks = append(tasks, jobs.ScheduledTask{
			Name:     taskMonthlyCreditGrant,
			Interval: billingCfg.GrantInterval,
			LockKey:  credits.LockKeyMonthlyGrant,
			Run:      grantRun,
		})
	}

	if reapRun != nil {
		tasks = append(tasks, jobs.ScheduledTask{
			Name:     taskPendingPaymentReaper,
			Interval: pendingPaymentReaperInterval,
			LockKey:  lockKeyPendingPaymentReaper,
			Run:      reapRun,
		})
	}
	return tasks
}

func buildScheduler(billingCfg config.BillingConfig, pool *db.Pool, components apiComponents, library *meetings.Service, billingService *billing.Service, logger zerolog.Logger) (*jobs.Scheduler, error) {
	var grantRun func(context.Context) error
	if billingCfg.FreeGrantMinutes > 0 {
		granter, err := credits.NewGranter(credits.GranterOptions{
			Pool:    pool.Pool(),
			Minutes: billingCfg.FreeGrantMinutes,
			Logger:  logger,
		})
		if err != nil {
			return nil, err
		}
		grantRun = func(ctx context.Context) error {
			run, err := granter.RunOnce(ctx)
			if err != nil {
				return err
			}
			logger.Info().
				Time("period", run.Period).
				Int("granted", run.Granted).
				Int64("granted_minutes", run.GrantedMinutes).
				Int("expired", run.Expired).
				Int64("expired_minutes", run.ExpiredMinutes).
				Msg("monthly credit grant complete")
			return nil
		}
	} else {
		logger.Warn().Msg("monthly credit grants are disabled: FREE_GRANT_MINUTES is zero")
	}

	var reapRun func(context.Context) error
	if billingService != nil {
		reapRun = func(ctx context.Context) error {
			reaped, err := billingService.ReapPendingPayments(ctx, billingCfg.PendingTTL)
			if err != nil {
				return err
			}
			if reaped > 0 {
				logger.Info().Int("payments", reaped).Msg("stale pending payments were failed")
			}
			return nil
		}
	}

	tasks := startupTasks(billingCfg, components.authStore, library, grantRun, reapRun)
	if len(tasks) == 0 {
		return nil, nil
	}

	scheduler := jobs.NewScheduler(pool.Pool(), jobs.WithSchedulerLogger(logger))
	for _, task := range tasks {
		if err := scheduler.Register(task); err != nil {
			return nil, err
		}
	}
	return scheduler, nil
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
