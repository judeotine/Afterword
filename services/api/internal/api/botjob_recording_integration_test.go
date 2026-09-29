//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/botjobs"
)

func TestWorkerRecordingCreatesAndAttachesMeeting(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	workspaceID := uuid.MustParse(owner.Workspace.ID)

	job, err := harness.botJobs.Create(t.Context(), workspaceID, botjobs.CreateParams{
		MeetingURL:       "https://meet.google.com/abc-defg-hij",
		Platform:         "meet",
		EstimatedMinutes: 30,
	})
	if err != nil {
		t.Fatalf("create bot job: %v", err)
	}

	path := "/v1/worker/bot-jobs/" + job.ID.String() + "/recording"
	body := map[string]any{
		"worker_id":       "worker-1",
		"title":           "Recorded standup",
		"duration_s":      600,
		"audio_extension": "opus",
		"size_bytes":      2048,
	}
	response := harness.call(http.MethodPost, path, body, withWorkerToken("test-worker-token"))
	if response.Status != http.StatusCreated {
		t.Fatalf("recording: status %d, body %s", response.Status, response.Body)
	}

	attached, err := harness.botJobs.GetByID(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get bot job: %v", err)
	}
	if attached.MeetingID == nil {
		t.Fatalf("expected a meeting to be attached to the bot job")
	}

	second := harness.call(http.MethodPost, path, body, withWorkerToken("test-worker-token"))
	if second.Status != http.StatusConflict {
		t.Fatalf("expected conflict on a second recording, got %d body %s", second.Status, second.Body)
	}
}

func TestWorkerRecordingRejectsMissingToken(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	workspaceID := uuid.MustParse(owner.Workspace.ID)

	job, err := harness.botJobs.Create(t.Context(), workspaceID, botjobs.CreateParams{
		MeetingURL:       "https://meet.google.com/abc-defg-hij",
		Platform:         "meet",
		EstimatedMinutes: 30,
	})
	if err != nil {
		t.Fatalf("create bot job: %v", err)
	}

	path := "/v1/worker/bot-jobs/" + job.ID.String() + "/recording"
	response := harness.call(http.MethodPost, path, map[string]any{"worker_id": "worker-1", "title": "No token"}, withWorkerToken("wrong-token"))
	if response.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d body %s", response.Status, response.Body)
	}
}
