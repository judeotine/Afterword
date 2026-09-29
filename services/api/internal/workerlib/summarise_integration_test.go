//go:build integration

package workerlib

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/jobs"
	"github.com/judeotine/afterword/services/api/internal/meetings"
	"github.com/judeotine/afterword/services/api/internal/storage"
	"github.com/judeotine/afterword/services/api/internal/storagetest"
)

func TestSummariseHandlerWritesASummaryRow(t *testing.T) {
	summariseBin := buildSummariseBinary(t)
	templatePath := repoTemplate(t)

	pool := dbtest.New(t)
	client, buckets := storagetest.New(t)
	queries := sqlcgen.New(pool)

	workspaceID := dbtest.NewWorkspace(t, pool)
	meeting, err := queries.CreateMeeting(t.Context(), sqlcgen.CreateMeetingParams{
		WorkspaceID:  workspaceID,
		Title:        "Summarise source",
		Source:       meetings.SourceDesktop,
		ConsentState: "granted",
		Visibility:   meetings.VisibilityPrivate,
		Status:       meetings.StatusReady,
	})
	if err != nil {
		t.Fatalf("create meeting: %v", err)
	}

	workDir := t.TempDir()
	transcriptPath := filepath.Join(workDir, "transcripts.json")
	body := `[{"id":"a","text":"We shipped the beta","timestamp":"2026-01-01T00:00:00Z","audio_start_time":0,"audio_end_time":2,"duration":2}]`
	if err := os.WriteFile(transcriptPath, []byte(body), 0o644); err != nil {
		t.Fatalf("write transcript fixture: %v", err)
	}
	transcriptKey := storage.TranscriptKey(workspaceID, meeting.ID)
	uploadFixture(t, client, buckets.Transcripts, transcriptKey, transcriptPath)

	deps := SummariseDeps{
		Queries:          queries,
		Storage:          client,
		TranscriptBucket: buckets.Transcripts,
		SummariseBin:     summariseBin,
		TemplatePath:     templatePath,
		Provider:         "offline",
		WorkDir:          workDir,
	}
	handler := NewSummariseHandler(deps)
	job := &jobs.Job{Kind: meetings.KindSummarise, Payload: mustJSON(t, meetings.TranscodePayload{
		WorkspaceID: workspaceID,
		MeetingID:   meeting.ID,
		Bucket:      buckets.Transcripts,
		Key:         transcriptKey,
		Generation:  1,
	})}
	if err := handler(t.Context(), job); err != nil {
		t.Fatalf("summarise handler: %v", err)
	}

	summaries, err := queries.ListSummariesByWorkspace(t.Context(), sqlcgen.ListSummariesByWorkspaceParams{
		WorkspaceID: workspaceID,
		MeetingID:   &meeting.ID,
		PageSize:    10,
	})
	if err != nil {
		t.Fatalf("list summaries: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	summary := summaries[0]
	if summary.Markdown == "" {
		t.Fatal("expected non-empty summary markdown")
	}
	if summary.Model != "offline" {
		t.Fatalf("expected model offline, got %q", summary.Model)
	}
}

func buildSummariseBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("cargo"); err != nil {
		t.Skipf("cargo not on PATH: %v", err)
	}
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "afterword-summarise")
	cmd := exec.CommandContext(t.Context(), "cargo", "build", "-p", "afterword-core", "--bin", "afterword-summarise")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("build afterword-summarise: %v: %s", err, string(output))
	}
	built := filepath.Join(root, "target", "debug", "afterword-summarise")
	data, err := os.ReadFile(built)
	if err != nil {
		t.Skipf("read built binary: %v", err)
	}
	if err := os.WriteFile(out, data, 0o755); err != nil {
		t.Fatalf("stage binary: %v", err)
	}
	return out
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "Cargo.toml")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("could not locate repo root with Cargo.toml")
	return ""
}

func repoTemplate(t *testing.T) string {
	t.Helper()
	path := filepath.Join(repoRoot(t), "frontend", "src-tauri", "templates", "standard_meeting.json")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("template not found: %v", err)
	}
	return path
}
