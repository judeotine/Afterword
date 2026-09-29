package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/judeotine/afterword/services/api/internal/botjobs"
	"github.com/judeotine/afterword/services/api/internal/httpx"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/validation"
)

type workerClaimRequest struct {
	WorkerID string `json:"worker_id"`
}

type workerStatusRequest struct {
	WorkerID         string `json:"worker_id"`
	Status           string `json:"status"`
	MinutesUsed      *int32 `json:"minutes_used"`
	ConsentAnnounced bool   `json:"consent_announced"`
	Error            string `json:"error"`
}

func (b *BotJobServer) handleWorkerClaim(w http.ResponseWriter, r *http.Request) {
	var body workerClaimRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	workerID := v.RequiredString("worker_id", body.WorkerID, 200)
	if v.Write(w, r) {
		return
	}

	job, err := b.botJobs.ClaimNext(r.Context(), workerID)
	if err != nil {
		if errors.Is(err, botjobs.ErrNoJobAvailable) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		b.writeBotJobError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newBotJobView(job))
}

type workerRecordingRequest struct {
	WorkerID       string `json:"worker_id"`
	Title          string `json:"title"`
	DurationS      int32  `json:"duration_s"`
	AudioExtension string `json:"audio_extension"`
	SizeBytes      int64  `json:"size_bytes"`
}

func (b *BotJobServer) handleWorkerRecording(w http.ResponseWriter, r *http.Request) {
	jobID, ok := pathUUID(r, botJobParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That bot job does not exist.")
		return
	}
	var body workerRecordingRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	workerID := v.RequiredString("worker_id", body.WorkerID, 200)
	title := v.RequiredString("title", body.Title, meetings.MaxTitleLength)
	v.Min("duration_s", int64(body.DurationS), 0)
	v.Min("size_bytes", body.SizeBytes, 0)
	if v.Write(w, r) {
		return
	}

	job, err := b.botJobs.GetByID(r.Context(), jobID)
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}
	if job.MeetingID != nil {
		httpx.WriteError(w, r, http.StatusConflict, "recording_exists", "That bot job already has a recording.")
		return
	}

	var startedAt *time.Time
	if !job.ScheduledAt.IsZero() {
		scheduled := job.ScheduledAt
		startedAt = &scheduled
	}

	created, err := b.meetings.CreateForWorker(r.Context(), meetings.WorkerCreateParams{
		WorkspaceID:    job.WorkspaceID,
		Title:          title,
		Platform:       job.Platform,
		StartedAt:      startedAt,
		DurationS:      body.DurationS,
		AudioExtension: body.AudioExtension,
		AudioSizeBytes: body.SizeBytes,
	})
	if err != nil {
		httpx.WriteError(w, r, http.StatusInternalServerError, "recording_failed", "The recording could not be stored.")
		return
	}

	if _, err := b.botJobs.AttachMeeting(r.Context(), jobID, created.Meeting.ID); err != nil {
		b.writeBotJobError(w, r, err)
		return
	}
	_, _ = b.botJobs.ReportStatus(r.Context(), jobID, workerID, botjobs.WorkerUpdate{})

	httpx.WriteJSON(w, r, http.StatusCreated, createMeetingView{
		Meeting: newMeetingView(created.Meeting),
		Upload:  newUploadView(created.Upload),
	})
}

func (b *BotJobServer) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	jobID, ok := pathUUID(r, botJobParam)
	if !ok {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That bot job does not exist.")
		return
	}
	var body workerStatusRequest
	if err := decodeJSON(w, r, &body); err != nil {
		return
	}
	v := validation.New()
	workerID := v.RequiredString("worker_id", body.WorkerID, 200)
	if v.Write(w, r) {
		return
	}

	job, err := b.botJobs.ReportStatus(r.Context(), jobID, workerID, botjobs.WorkerUpdate{
		Status:           body.Status,
		MinutesUsed:      body.MinutesUsed,
		ConsentAnnounced: body.ConsentAnnounced,
		Error:            body.Error,
	})
	if err != nil {
		b.writeBotJobError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newBotJobView(job))
}
