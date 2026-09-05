package storage_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestMeetingKeysFollowTheWorkspaceScheme(t *testing.T) {
	workspace := uuid.MustParse("11111111-1111-4111-8111-111111111111")
	meeting := uuid.MustParse("22222222-2222-4222-8222-222222222222")

	audio := storage.AudioKey(workspace, meeting, "opus")
	want := "ws/11111111-1111-4111-8111-111111111111/meetings/22222222-2222-4222-8222-222222222222/audio.opus"
	if audio != want {
		t.Fatalf("audio key = %q, want %q", audio, want)
	}

	transcript := storage.TranscriptKey(workspace, meeting)
	want = "ws/11111111-1111-4111-8111-111111111111/meetings/22222222-2222-4222-8222-222222222222/transcript.json"
	if transcript != want {
		t.Fatalf("transcript key = %q, want %q", transcript, want)
	}
}

func TestAudioKeyFallsBackToTheDefaultExtension(t *testing.T) {
	workspace := uuid.New()
	meeting := uuid.New()

	for _, extension := range []string{"", "   ", ".", "../evil", "WAV/x"} {
		key := storage.AudioKey(workspace, meeting, extension)
		if key != storage.AudioKey(workspace, meeting, storage.DefaultAudioExtension) {
			t.Fatalf("extension %q produced %q", extension, key)
		}
	}
}

func TestAudioKeyNormalisesAnAcceptedExtension(t *testing.T) {
	workspace := uuid.New()
	meeting := uuid.New()

	if got, want := storage.AudioKey(workspace, meeting, ".WAV"), storage.AudioKey(workspace, meeting, "wav"); got != want {
		t.Fatalf("normalised key = %q, want %q", got, want)
	}
}

func TestMeetingPrefixCoversEveryMeetingObject(t *testing.T) {
	workspace := uuid.New()
	meeting := uuid.New()

	prefix := storage.MeetingPrefix(workspace, meeting)
	for _, key := range []string{storage.AudioKey(workspace, meeting, "opus"), storage.TranscriptKey(workspace, meeting)} {
		if len(key) <= len(prefix) || key[:len(prefix)] != prefix {
			t.Fatalf("key %q is not under prefix %q", key, prefix)
		}
	}
}

func TestBucketsFillInTheDefaults(t *testing.T) {
	buckets := storage.Buckets{}.WithDefaults()
	if buckets.Audio != "audio" || buckets.Transcripts != "transcripts" || buckets.Clips != "clips" || buckets.Exports != "exports" {
		t.Fatalf("unexpected defaults: %+v", buckets)
	}

	custom := storage.Buckets{Audio: "recordings"}.WithDefaults()
	if custom.Audio != "recordings" || custom.Transcripts != "transcripts" {
		t.Fatalf("unexpected overrides: %+v", custom)
	}
}

func TestBucketsListsEveryConfiguredBucket(t *testing.T) {
	names := storage.Buckets{}.WithDefaults().Names()
	if len(names) != 4 {
		t.Fatalf("expected four buckets, got %v", names)
	}
}
