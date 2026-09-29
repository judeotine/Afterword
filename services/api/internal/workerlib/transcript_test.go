package workerlib

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseTranscriptsOrdersAndSequences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcripts.json")
	body := `[
		{"text":"second","audio_start_time":5.0,"audio_end_time":7.5},
		{"text":"  ","audio_start_time":1.0,"audio_end_time":2.0},
		{"text":"first","audio_start_time":0.0,"audio_end_time":4.0}
	]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	segments, err := parseTranscripts(path)
	if err != nil {
		t.Fatalf("parseTranscripts: %v", err)
	}
	if len(segments) != 2 {
		t.Fatalf("expected 2 segments after dropping blank, got %d", len(segments))
	}
	if segments[0].Text != "first" || segments[0].Seq != 0 {
		t.Fatalf("expected first segment resequenced to 0, got %+v", segments[0])
	}
	if segments[1].Text != "second" || segments[1].Seq != 1 {
		t.Fatalf("expected second segment resequenced to 1, got %+v", segments[1])
	}
	if got := transcriptDurationSeconds(segments); got != 7.5 {
		t.Fatalf("expected duration 7.5, got %v", got)
	}
}

func TestParseTranscriptsClampsReversedBounds(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcripts.json")
	body := `[{"text":"x","audio_start_time":9.0,"audio_end_time":3.0}]`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	segments, err := parseTranscripts(path)
	if err != nil {
		t.Fatalf("parseTranscripts: %v", err)
	}
	if len(segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segments))
	}
	if segments[0].EndS < segments[0].StartS {
		t.Fatalf("expected end clamped to start, got %+v", segments[0])
	}
}
