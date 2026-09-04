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
)

type BotJobOptions struct {
	BotJobs      *botjobs.Service
	Entitlements *billing.Entitlements
	Middleware   *auth.Middleware
}

type BotJobServer struct {
	botJobs      *botjobs.Service
	entitlements *billing.Entitlements
	middleware   *auth.Middleware
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
	}, nil
}

func (b *BotJobServer) Routes(router chi.Router) {
	router.Group(func(r chi.Router) {
		r.Use(b.middleware.RequireAuth)
		r.Use(b.middleware.RequireWorkspace)
		r.Post(botJobsPath, b.handleCreateBotJob)
		r.Get(botJobsPath, b.handleListBotJobs)
		r.Get(botJobPath, b.handleGetBotJob)
	})
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
	{botjobs.ErrInvalidCursor, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That page cursor is not valid."}},
	{billing.ErrInvalidEstimate, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That is not a usable estimate in minutes."}},
}
