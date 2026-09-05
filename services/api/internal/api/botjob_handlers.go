package api

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/botjobs"
	"github.com/judeotine/afterword/services/api/internal/credits"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

func (b *BotJobServer) writeBotJobError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, credits.ErrInsufficientCredits) {
		writeInsufficientCredits(w, r, topUpURLFor(err, b.entitlements.TopUpURL()))
		return
	}
	for _, candidate := range botJobErrors {
		if errors.Is(err, candidate.target) {
			httpx.WriteError(w, r, candidate.result.status, candidate.result.code, candidate.result.message)
			return
		}
	}
	hlog.FromRequest(r).Error().Err(err).Msg("bot job request failed")
	httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternalError, genericInternalMessage)
}

func (b *BotJobServer) workspace(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		hlog.FromRequest(r).Error().Msg("a bot job route ran without require workspace")
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return uuid.Nil, false
	}
	return membership.WorkspaceID, true
}

type createBotJobRequest struct {
	MeetingURL       string  `json:"meeting_url"`
	Platform         string  `json:"platform"`
	BotName          string  `json:"bot_name"`
	ScheduledAt      *string `json:"scheduled_at"`
	EstimatedMinutes *int32  `json:"estimated_minutes"`
}

func (b *BotJobServer) handleCreateBotJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	var body createBotJobRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	v := validation.New()
	meetingURL := v.RequiredString("meeting_url", body.MeetingURL, botjobs.MaxURLLength)
	platform := strings.ToLower(strings.TrimSpace(body.Platform))
	if platform != "" {
		platform = v.OneOf("platform", platform, botjobs.Platforms, "")
	}
	botName := v.OptionalString("bot_name", body.BotName, botjobs.MaxBotNameLength)

	estimate := billing.DefaultBotEstimate
	if body.EstimatedMinutes != nil {
		estimate = int32(v.Min("estimated_minutes", int64(*body.EstimatedMinutes), 1))
		estimate = int32(v.Max("estimated_minutes", int64(estimate), int64(billing.MaxEstimateMinutes)))
	}

	var scheduledAt *time.Time
	if body.ScheduledAt != nil {
		parsed, err := time.Parse(time.RFC3339, *body.ScheduledAt)
		if err != nil {
			v.Add("scheduled_at", "must be an RFC 3339 timestamp")
		} else {
			utc := parsed.UTC()
			scheduledAt = &utc
		}
	}
	if v.Write(w, r) {
		return
	}

	params, err := b.botJobs.Resolve(botjobs.CreateParams{
		MeetingURL:       meetingURL,
		Platform:         platform,
		BotName:          botName,
		ScheduledAt:      scheduledAt,
		EstimatedMinutes: estimate,
	})
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}

	if err := b.entitlements.RequireCredits(r.Context(), workspaceID, params.EstimatedMinutes); err != nil {
		b.writeBotJobError(w, r, err)
		return
	}

	job, err := b.botJobs.Create(r.Context(), workspaceID, params)
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusCreated, newBotJobView(job))
}

func (b *BotJobServer) handleListBotJobs(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	v := validation.New()
	pageSize := queryInt32(v, r, "limit", botjobs.DefaultPageSize, botjobs.MaxPageSize)
	if v.Write(w, r) {
		return
	}

	jobs, next, err := b.botJobs.List(r.Context(), workspaceID, queryString(r, "cursor"), pageSize)
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}

	view := botJobListView{BotJobs: make([]botJobView, 0, len(jobs)), NextCursor: next}
	for _, job := range jobs {
		view.BotJobs = append(view.BotJobs, newBotJobView(job))
	}
	httpx.WriteJSON(w, r, http.StatusOK, view)
}

func (b *BotJobServer) handleGetBotJob(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := b.workspace(w, r)
	if !ok {
		return
	}

	jobID, ok := pathUUID(r, botJobParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That bot job does not exist.")
		return
	}

	job, err := b.botJobs.Get(r.Context(), workspaceID, jobID)
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newBotJobView(job))
}
