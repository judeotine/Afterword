//go:build integration

package workerlib

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/storagetest"
)

func TestTranscribeHandlerProcessesAMeetingEndToEnd(t *testing.T) {
	requireBinary(t, "ffmpeg")

	pool := dbtest.New(t)
	client, buckets := storagetest.New(t)
	queries := sqlcgen.New(pool)

	workspaceID := dbtest.NewWorkspace(t, pool)
	meeting, err := queries.CreateMeeting(t.Context(), sqlcgen.CreateMeetingParams{
		WorkspaceID:  workspaceID,
		Title:        "End to end transcription",
		Source:       meetings.SourceDesktop,
		ConsentState: "granted",
		Visibility:   meetings.VisibilityPrivate,
		Status:       meetings.StatusProcessing,
	})
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}

	workDir := t.TempDir()
	wavPath := filepath.Join(workDir, "source.wav")
	synthesizeWav(t, wavPath)

	audioKey := storage.AudioKey(workspaceID, meeting.ID, "wav")
	uploadFixture(t, client, buckets.Audio, audioKey, wavPath)

	fakeBin := writeFakeTranscribe(t)

	deps := Deps{
		Queries: queries,
		Storage: client,
		Config: Config{
			AudioBucket:      buckets.Audio,
			TranscriptBucket: buckets.Transcripts,
			TranscribeBin:    fakeBin,
			FFmpegBin:        "ffmpeg",
			Engine:           "whisper",
			Model:            "base",
			ModelsDir:        workDir,
			WorkDir:          workDir,
		},
		Logger: zerolog.Nop(),
	}

	handler := NewTranscribeHandler(deps)
	payload := meetings.TranscodePayload{
		WorkspaceID: workspaceID,
		MeetingID:   meeting.ID,
		Bucket:      buckets.Audio,
		Key:         audioKey,
		Generation:  1,
	}
	job := &jobs.Job{Kind: meetings.KindTranscribe, Payload: mustJSON(t, payload)}
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("transcribe handler: %v", err)
	}

	segments, err := queries.ListSegmentsBySeq(t.Context(), sqlcgen.ListSegmentsBySeqParams{
		MeetingID:   meeting.ID,
		WorkspaceID: workspaceID,
		PageSize:    100,
	})
	if err != nil {
		t.Fatalf("list segments: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segments))
	}
	if segments[0].Text != "hello world" || segments[1].Text != "second line" {
		t.Fatalf("unexpected segment text: %+v", segments)
	}

	updated, err := queries.GetMeeting(t.Context(), sqlcgen.GetMeetingParams{ID: meeting.ID, WorkspaceID: workspaceID})
	if err != nil {
		t.Fatalf("get meeting: %v", err)
	}
	if updated.Status != meetings.StatusReady {
		t.Fatalf("expected status ready, got %q", updated.Status)
	}
	if updated.TranscriptObject == nil || *updated.TranscriptObject == "" {
		t.Fatal("expected a transcript object on the meeting")
	}
	opusKey := storage.AudioKey(workspaceID, meeting.ID, "opus")
	if updated.AudioObject == nil || *updated.AudioObject != opusKey {
		t.Fatalf("expected audio object %q, got %v", opusKey, updated.AudioObject)
	}
	if _, err := client.Head(t.Context(), buckets.Audio, opusKey); err != nil {
		t.Fatalf("opus object missing from storage: %v", err)
	}
	if _, err := client.Head(t.Context(), buckets.Audio, audioKey); err == nil {
		t.Fatal("expected the original wav object to be deleted after transcode")
	}
	if _, err := client.Head(t.Context(), buckets.Transcripts, *updated.TranscriptObject); err != nil {
		t.Fatalf("transcript object missing from storage: %v", err)
	}
}

func requireBinary(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not on PATH: %v", name, err)
	}
}

func synthesizeWav(t *testing.T, path string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "ffmpeg", "-y", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-ar", "16000", "-ac", "1", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthesize wav: %v: %s", err, string(output))
	}
}

func uploadFixture(t *testing.T, client storage.Client, bucket, key, srcPath string) {
	t.Helper()
	if err := upload(t.Context(), client, bucket, key, srcPath); err != nil {
		t.Fatalf("upload fixture: %v", err)
	}
}

func writeFakeTranscribe(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-transcribe.sh")
	body := `#!/usr/bin/env bash
set -euo pipefail
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --out) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
mkdir -p "$out"
cat > "$out/transcripts.json" <<'JSON'
[
  {"id":"a","text":"hello world","timestamp":"2026-01-01T00:00:00Z","audio_start_time":0.0,"audio_end_time":1.0,"duration":1.0},
  {"id":"b","text":"second line","timestamp":"2026-01-01T00:00:01Z","audio_start_time":1.0,"audio_end_time":2.0,"duration":1.0}
]
JSON
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write fake transcribe: %v", err)
	}
	return script
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return data
}
