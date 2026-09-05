//go:build integration

package api_test

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
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

func TestADeclaredUploadSizeIsSignedIntoThePutUrl(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Declared size",
		"source":     "desktop",
		"size_bytes": 2048,
	})
	if created.Upload.SizeBytes != 2048 {
		t.Fatalf("size_bytes = %d, want 2048", created.Upload.SizeBytes)
	}
	if created.Upload.MaxBytes == 0 {
		t.Fatal("max_bytes is no longer advertised alongside a declared size")
	}

	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	if err := harness.memory.PutSigned("audio", audioKey, make([]byte, 1024), "audio/opus"); !errors.Is(err, storage.ErrSizeMismatch) {
		t.Fatalf("a short upload against a signed size = %v, want ErrSizeMismatch", err)
	}
	if harness.memory.Exists("audio", audioKey) {
		t.Fatal("the refused upload was stored")
	}
	if err := harness.memory.PutSigned("audio", audioKey, make([]byte, 2048), "audio/opus"); err != nil {
		t.Fatalf("the exact upload was refused: %v", err)
	}

	finalized := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}
}

func TestAnOmittedSizeKeepsTheUnboundedUpload(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "No size", "source": "desktop"})
	if created.Upload.SizeBytes != 0 {
		t.Fatalf("size_bytes = %d, want it absent", created.Upload.SizeBytes)
	}
	if created.Upload.MaxBytes == 0 {
		t.Fatal("max_bytes was not advertised")
	}

	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	if err := harness.memory.PutSigned("audio", audioKey, make([]byte, 4096), "audio/opus"); err != nil {
		t.Fatalf("an unsigned upload was refused: %v", err)
	}
	if !harness.memory.Exists("audio", audioKey) {
		t.Fatal("the unsigned upload was not stored")
	}
}

func TestADeclaredSizeAboveTheCeilingIsRefused(t *testing.T) {
	harness := newLibraryHarnessWithLimits(t, 4096, 4096)
	owner := harness.signIn("owner@example.com")

	refused := harness.call(http.MethodPost, "/v1/meetings", map[string]any{
		"title":      "Too big",
		"source":     "desktop",
		"size_bytes": 8192,
	}, harness.as(owner)...)
	if refused.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized declaration: status %d, body %s", refused.Status, refused.Body)
	}
	if code := refused.errorCode(t); code != "object_too_large" {
		t.Fatalf("code %q, want object_too_large", code)
	}

	negative := harness.call(http.MethodPost, "/v1/meetings", map[string]any{
		"title":      "Negative",
		"source":     "desktop",
		"size_bytes": -1,
	}, harness.as(owner)...)
	if negative.Status != http.StatusBadRequest {
		t.Fatalf("negative declaration: status %d, body %s", negative.Status, negative.Body)
	}
}

func TestReissuedUploadUrlsTakeANewDeclaredSize(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")
	created := harness.createMeeting(owner, map[string]any{"title": "Resized", "source": "desktop"})

	again := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/upload-urls",
		map[string]any{"size_bytes": 512}, harness.as(owner)...)
	if again.Status != http.StatusOK {
		t.Fatalf("reissue: status %d, body %s", again.Status, again.Body)
	}
	var reissued createMeetingPayload
	again.decode(t, &reissued)
	if reissued.Upload.SizeBytes != 512 {
		t.Fatalf("reissued size_bytes = %d, want 512", reissued.Upload.SizeBytes)
	}

	audioKey := fmt.Sprintf("ws/%s/meetings/%s/audio.opus", owner.Workspace.ID, created.Meeting.ID)
	if err := harness.memory.PutSigned("audio", audioKey, make([]byte, 256), "audio/opus"); !errors.Is(err, storage.ErrSizeMismatch) {
		t.Fatalf("a short upload against the reissued size = %v, want ErrSizeMismatch", err)
	}
	if err := harness.memory.PutSigned("audio", audioKey, make([]byte, 512), "audio/opus"); err != nil {
		t.Fatalf("the exact upload was refused: %v", err)
	}
}
