package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/botjobs"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const (
	botJobsPath   = "/v1/bot-jobs"
	botJobPath    = "/v1/bot-jobs/{" + botJobParam + "}"
	botJobParam   = "botJobID"
	codeBadJobURL = "invalid_meeting_url"

	workerClaimPath  = "/v1/worker/bot-jobs/claim"
	workerStatusPath = "/v1/worker/bot-jobs/{" + botJobParam + "}/status"
)

type BotJobOptions struct {
	BotJobs      *botjobs.Service
	Entitlements *billing.Entitlements
	Middleware   *auth.Middleware
	WorkerAuth   *auth.WorkerAuth
}

type BotJobServer struct {
	botJobs      *botjobs.Service
	entitlements *billing.Entitlements
	middleware   *auth.Middleware
	workerAuth   *auth.WorkerAuth
}

func NewBotJobServer(options BotJobOptions) (*BotJobServer, error) {
	switch {
	case options.BotJobs == nil:
		return nil, errors.New("api: a bot job service is required")
	case options.Entitlements == nil:
		return nil, errors.New("api: a credit entitlement helper is required")
	case options.Middleware == nil:
		return nil, errors.New("api: auth middleware is required")
	}
	return &BotJobServer{
		botJobs:      options.BotJobs,
		entitlements: options.Entitlements,
		middleware:   options.Middleware,
		workerAuth:   options.WorkerAuth,
	}, nil
}

func (b *BotJobServer) Routes(router chi.Router) {
	router.Group(func(r chi.Router) {
		r.Use(b.middleware.RequireAuth)
		r.Use(b.middleware.RequireWorkspace)
		r.Get(botJobsPath, b.handleListBotJobs)
		r.Get(botJobPath, b.handleGetBotJob)

		r.Group(func(r chi.Router) {
			r.Use(b.middleware.RequireRole(auth.RoleAdmin))
			r.Post(botJobsPath, b.handleCreateBotJob)
		})
	})

	if b.workerAuth != nil && b.workerAuth.Enabled() {
		router.Group(func(r chi.Router) {
			r.Use(b.workerAuth.Require)
			r.Post(workerClaimPath, b.handleWorkerClaim)
			r.Post(workerStatusPath, b.handleWorkerStatus)
		})
	}
}

func RegisterBotJobRoutes(router chi.Router, options BotJobOptions) error {
	server, err := NewBotJobServer(options)
	if err != nil {
		return err
	}
	server.Routes(router)
	return nil
}

var botJobErrors = []struct {
	target error
	result statusError
}{
	{botjobs.ErrJobNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That bot job does not exist."}},
	{botjobs.ErrInvalidURL, statusError{http.StatusBadRequest, codeBadJobURL, "That meeting link is not a usable https url."}},
	{botjobs.ErrUnknownPlatform, statusError{http.StatusBadRequest, codeBadJobURL, "That meeting link is not a Google Meet, Zoom or Teams link."}},
	{botjobs.ErrPlatformMismatch, statusError{http.StatusBadRequest, codeBadJobURL, "That meeting link does not belong to the platform you named."}},
	{botjobs.ErrScheduleTooFar, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "A bot cannot be scheduled more than ninety days ahead."}},
	{botjobs.ErrScheduleInPast, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "A bot cannot be scheduled in the past."}},
	{botjobs.ErrInvalidCursor, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That page cursor is not valid."}},
	{billing.ErrInvalidEstimate, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That is not a usable estimate in minutes."}},
}
