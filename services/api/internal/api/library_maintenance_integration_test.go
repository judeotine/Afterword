//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func TestAbandonedPendingUploadsArePurgedAndReadyMeetingsAreNot(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	abandoned := harness.createMeeting(owner, map[string]any{"title": "Never uploaded", "source": "desktop"})
	abandonedAudio := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, abandoned.Meeting.ID)
	harness.memory.Put("audio", abandonedAudio, make([]byte, 64), "audio/opus")

	kept := harness.createMeeting(owner, map[string]any{"title": "Uploaded", "source": "desktop"})
	keptAudio := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, kept.Meeting.ID)
	harness.memory.Put("audio", keptAudio, make([]byte, 2048), "audio/opus")
	if finalized := harness.call(http.MethodPost, "/v1/meetings/"+kept.Meeting.ID+"/finalize", nil, harness.as(owner)...); finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}

	fresh := harness.createMeeting(owner, map[string]any{"title": "Still uploading", "source": "desktop"})

	if _, err := harness.pool.Exec(t.Context(),
		`UPDATE meetings SET created_at = now() - interval '48 hours' WHERE id = ANY($1::uuid[])`,
		[]string{abandoned.Meeting.ID, kept.Meeting.ID},
	); err != nil {
		t.Fatalf("backdate meetings: %v", err)
	}

	swept, err := harness.service.SweepAbandonedUploads(t.Context())
	if err != nil {
		t.Fatalf("SweepAbandonedUploads: %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept %d meetings, want 1", swept)
	}

	if got := harness.call(http.MethodGet, "/v1/meetings/"+abandoned.Meeting.ID, nil, harness.as(owner)...); got.Status != http.StatusNotFound {
		t.Fatalf("the abandoned meeting survived the sweep: status %d", got.Status)
	}
	if got := harness.call(http.MethodGet, "/v1/meetings/"+kept.Meeting.ID, nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("the ready meeting was swept: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.call(http.MethodGet, "/v1/meetings/"+fresh.Meeting.ID, nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("a pending meeting inside the ttl was swept: status %d, body %s", got.Status, got.Body)
	}

	key := "purge:meeting:" + abandoned.Meeting.ID
	if !harness.jobExists(t, meetings.KindPurge, key) {
		t.Fatal("no purge job was enqueued for the abandoned meeting")
	}
	job, err := harness.queue.GetByIdempotencyKey(t.Context(), meetings.KindPurge, key)
	if err != nil {
		t.Fatalf("read purge job: %v", err)
	}
	if err := meetings.NewPurgeHandler(harness.store)(t.Context(), job); err != nil {
		t.Fatalf("run purge handler: %v", err)
	}
	if harness.memory.Exists("audio", abandonedAudio) {
		t.Fatalf("the abandoned audio object was left behind: %v", harness.memory.Keys())
	}
	if !harness.memory.Exists("audio", keptAudio) {
		t.Fatal("the sweep purged the audio of a ready meeting")
	}
}

func TestAbandonedUploadSweepHonoursTheConfiguredTTL(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	meeting := harness.createMeeting(owner, map[string]any{"title": "Recent", "source": "desktop"})
	if _, err := harness.pool.Exec(t.Context(),
		`UPDATE meetings SET created_at = now() - interval '2 hours' WHERE id = $1`,
		meeting.Meeting.ID,
	); err != nil {
		t.Fatalf("backdate meeting: %v", err)
	}

	swept, err := harness.service.SweepAbandonedUploads(t.Context())
	if err != nil {
		t.Fatalf("SweepAbandonedUploads: %v", err)
	}
	if swept != 0 {
		t.Fatalf("swept %d meetings, want 0", swept)
	}
	if got := harness.call(http.MethodGet, "/v1/meetings/"+meeting.Meeting.ID, nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("a two hour old pending meeting was swept: status %d", got.Status)
	}
}
