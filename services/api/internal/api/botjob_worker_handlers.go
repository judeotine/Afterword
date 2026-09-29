package api

import (
	"errors"
	"net/http"

	"github.com/judeotine/afterword/services/api/internal/botjobs"
	"github.com/judeotine/afterword/services/api/internal/httpx"
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
