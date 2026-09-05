package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/botjobs"
)

type botJobView struct {
	ID               uuid.UUID `json:"id"`
	WorkspaceID      uuid.UUID `json:"workspace_id"`
	MeetingURL       string    `json:"meeting_url"`
	Platform         string    `json:"platform"`
	BotName          string    `json:"bot_name,omitempty"`
	ScheduledAt      time.Time `json:"scheduled_at"`
	Status           string    `json:"status"`
	EstimatedMinutes int32     `json:"estimated_minutes"`
	MinutesUsed      int32     `json:"minutes_used"`
	Error            string    `json:"error,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type botJobListView struct {
	BotJobs    []botJobView `json:"bot_jobs"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func newBotJobView(job botjobs.BotJob) botJobView {
	return botJobView{
		ID:               job.ID,
		WorkspaceID:      job.WorkspaceID,
		MeetingURL:       job.MeetingURL,
		Platform:         job.Platform,
		BotName:          job.BotName,
		ScheduledAt:      job.ScheduledAt,
		Status:           job.Status,
		EstimatedMinutes: job.EstimatedMinutes,
		MinutesUsed:      job.MinutesUsed,
		Error:            job.Error,
		CreatedAt:        job.CreatedAt,
	}
}
