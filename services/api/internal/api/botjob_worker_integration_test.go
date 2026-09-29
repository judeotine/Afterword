//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

func withWorkerToken(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(auth.WorkerTokenHeader, token)
	}
}

func TestWorkerClaimsAndReportsBotJob(t *testing.T) {
	h := newBillingHarness(t)
	session := h.signIn("worker-flow@example.com")
	workspaceID := uuid.MustParse(session.Workspace.ID)
	h.grant(workspaceID, 120)

	created := h.createBotJob(session, map[string]any{"meeting_url": "https://meet.google.com/abc-defg-hij"})
	if created.Status != http.StatusCreated {
		t.Fatalf("create bot job: status %d, body %s", created.Status, created.Body)
	}
	var job botJobPayload
	created.decode(t, &job)

	noAuth := h.do(http.MethodPost, "/v1/worker/bot-jobs/claim", map[string]any{"worker_id": "w1"})
	if noAuth.Status != http.StatusUnauthorized {
		t.Fatalf("claim without worker token: status %d", noAuth.Status)
	}

	claimed := h.do(http.MethodPost, "/v1/worker/bot-jobs/claim", map[string]any{"worker_id": "w1"}, withWorkerToken("test-worker-token"))
	if claimed.Status != http.StatusOK {
		t.Fatalf("claim: status %d, body %s", claimed.Status, claimed.Body)
	}
	var claimedJob botJobPayload
	claimed.decode(t, &claimedJob)
	if claimedJob.ID != job.ID {
		t.Fatalf("claimed job %s, want %s", claimedJob.ID, job.ID)
	}
	if claimedJob.Status != "claimed" {
		t.Fatalf("claimed status = %q, want claimed", claimedJob.Status)
	}

	empty := h.do(http.MethodPost, "/v1/worker/bot-jobs/claim", map[string]any{"worker_id": "w1"}, withWorkerToken("test-worker-token"))
	if empty.Status != http.StatusNoContent {
		t.Fatalf("second claim should find nothing: status %d", empty.Status)
	}

	statusPath := "/v1/worker/bot-jobs/" + job.ID + "/status"
	recording := h.do(http.MethodPost, statusPath, map[string]any{
		"worker_id": "w1", "status": "recording", "consent_announced": true,
	}, withWorkerToken("test-worker-token"))
	if recording.Status != http.StatusOK {
		t.Fatalf("report recording: status %d, body %s", recording.Status, recording.Body)
	}
	var recordingJob botJobPayload
	recording.decode(t, &recordingJob)
	if recordingJob.Status != "recording" {
		t.Fatalf("status = %q, want recording", recordingJob.Status)
	}

	minutes := int32(12)
	done := h.do(http.MethodPost, statusPath, map[string]any{
		"worker_id": "w1", "status": "completed", "minutes_used": minutes,
	}, withWorkerToken("test-worker-token"))
	if done.Status != http.StatusOK {
		t.Fatalf("report completed: status %d, body %s", done.Status, done.Body)
	}
	var doneJob botJobPayload
	done.decode(t, &doneJob)
	if doneJob.Status != "completed" || doneJob.MinutesUsed != 12 {
		t.Fatalf("final job = %+v", doneJob)
	}

	wrongWorker := h.do(http.MethodPost, statusPath, map[string]any{
		"worker_id": "someone-else", "status": "failed",
	}, withWorkerToken("test-worker-token"))
	if wrongWorker.Status != http.StatusNotFound {
		t.Fatalf("status from a different worker should not match: status %d", wrongWorker.Status)
	}
}
