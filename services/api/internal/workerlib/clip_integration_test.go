//go:build integration

package workerlib

import (
	"testing"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/storagetest"
)

func TestClipHandlerCutsAndStoresAnOpusClip(t *testing.T) {
	requireBinary(t, "ffmpeg")

	pool := dbtest.New(t)
	client, buckets := storagetest.New(t)
	queries := sqlcgen.New(pool)

	workspaceID := dbtest.NewWorkspace(t, pool)
	meeting, err := queries.CreateMeeting(t.Context(), sqlcgen.CreateMeetingParams{
		WorkspaceID:  workspaceID,
		Title:        "Clip source",
		Source:       meetings.SourceDesktop,
		ConsentState: "granted",
		Visibility:   meetings.VisibilityPrivate,
		Status:       meetings.StatusReady,
	})
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}

	workDir := t.TempDir()
	sourceWav := workDir + "/source.wav"
	synthesizeWav(t, sourceWav)
	sourceKey := storage.AudioKey(workspaceID, meeting.ID, "wav")
	uploadFixture(t, client, buckets.Audio, sourceKey, sourceWav)

	token := "clip-token"
	clip, err := queries.CreateClip(t.Context(), sqlcgen.CreateClipParams{
		MeetingID:   meeting.ID,
		StartS:      0.2,
		EndS:        0.8,
		Title:       "Snippet",
		ShareToken:  &token,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		t.Fatalf("create clip: %v", err)
	}
	targetKey := storage.ClipKey(workspaceID, meeting.ID, clip.ID)

	deps := ClipDeps{
		Queries:     queries,
		Storage:     client,
		AudioBucket: buckets.Audio,
		ClipBucket:  buckets.Clips,
		FFmpegBin:   "ffmpeg",
		WorkDir:     workDir,
	}
	handler := NewClipHandler(deps)
	job := &jobs.Job{Kind: meetings.KindClip, Payload: mustJSON(t, meetings.ClipPayload{
		WorkspaceID: workspaceID,
		MeetingID:   meeting.ID,
		ClipID:      clip.ID,
		SourceKey:   sourceKey,
		TargetKey:   targetKey,
		StartS:      0.2,
		EndS:        0.8,
	})}
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("clip handler: %v", err)
	}

	if _, err := client.Head(t.Context(), buckets.Clips, targetKey); err != nil {
		t.Fatalf("clip object missing from storage: %v", err)
	}

	updated, err := queries.GetClip(t.Context(), sqlcgen.GetClipParams{ID: clip.ID, WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("get clip: %v", err)
	}
	if updated.Object == nil || *updated.Object != targetKey {
		t.Fatalf("expected clip object %q, got %v", targetKey, updated.Object)
	}
}
