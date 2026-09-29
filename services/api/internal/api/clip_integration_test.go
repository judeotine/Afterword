//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestClipsLifecycle(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner@example.com")

	meeting := harness.createMeeting(owner, map[string]any{
		"title": "Long call", "source": "desktop", "duration_s": 600,
	})
	memory.Put("audio", "ws/"+owner.Workspace.ID+"/meetings/"+meeting.Meeting.ID+"/audio.opus", make([]byte, 4096), "audio/opus")
	memory.Put("transcripts", "ws/"+owner.Workspace.ID+"/meetings/"+meeting.Meeting.ID+"/transcript.json", []byte(`{"segments":[]}`), "application/json")
	finalized := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}

	created := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/clips", map[string]any{
		"start_s": 10, "end_s": 25, "title": "Key moment",
	}, harness.as(owner)...)
	if created.Status != http.StatusCreated {
		t.Fatalf("create clip: status %d, body %s", created.Status, created.Body)
	}
	var clip struct {
		ID         string  `json:"id"`
		StartS     float64 `json:"start_s"`
		EndS       float64 `json:"end_s"`
		Title      string  `json:"title"`
		ShareToken string  `json:"share_token"`
		Ready      bool    `json:"ready"`
	}
	created.decode(t, &clip)
	if clip.StartS != 10 || clip.EndS != 25 || clip.Title != "Key moment" {
		t.Fatalf("unexpected clip: %+v", clip)
	}
	if clip.ShareToken == "" {
		t.Fatal("expected a share token on the clip")
	}
	if clip.Ready {
		t.Fatal("clip should not be ready before the cut job runs")
	}

	if !harness.jobExists(t, meetings.KindClip, "clip:"+clip.ID) {
		t.Fatal("expected a clip cut job to be enqueued")
	}

	badBounds := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/clips", map[string]any{
		"start_s": 30, "end_s": 20,
	}, harness.as(owner)...)
	if badBounds.Status != http.StatusBadRequest {
		t.Fatalf("reversed bounds should be rejected: status %d", badBounds.Status)
	}

	listed := harness.call(http.MethodGet, "/v1/meetings/"+meeting.Meeting.ID+"/clips", nil, harness.as(owner)...)
	var list struct {
		Clips []struct {
			ID string `json:"id"`
		} `json:"clips"`
	}
	listed.decode(t, &list)
	if len(list.Clips) != 1 || list.Clips[0].ID != clip.ID {
		t.Fatalf("expected one clip, got %+v", list.Clips)
	}

	removed := harness.call(http.MethodDelete, "/v1/meetings/"+meeting.Meeting.ID+"/clips/"+clip.ID, nil, harness.as(owner)...)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("delete clip: status %d, body %s", removed.Status, removed.Body)
	}
}
