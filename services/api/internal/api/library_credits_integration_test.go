//go:build integration

package api_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func audioKey(workspaceID, meetingID string) string {
	return fmt.Sprintf("ws/%s/meetings/%s/audio.opus", workspaceID, meetingID)
}

func transcriptKey(workspaceID, meetingID string) string {
	return fmt.Sprintf("ws/%s/meetings/%s/transcript.json", workspaceID, meetingID)
}

func bytesForMinutes(minutes int64) int {
	return int(meetings.MaxAudioBytesPerSecond*meetings.SecondsPerMinute*(minutes-1)) + 1
}

func TestBillableMinutesComeFromTheStoredObjectNotTheDeclaredDuration(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signInBroke("declared-zero@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":  "Three hours declared as nothing",
		"source": "desktop",
	})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID),
		make([]byte, bytesForMinutes(3)), "audio/opus")

	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"

	broke := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if broke.Status != http.StatusPaymentRequired {
		t.Fatalf("a zero-declared-duration upload was transcribed for free: status %d, body %s", broke.Status, broke.Body)
	}
	var refused entitlementPayload
	broke.decode(t, &refused)
	if refused.Error.Code != "insufficient_credits" || refused.Error.TopUpURL == "" {
		t.Fatalf("error body = %+v", refused.Error)
	}
	if harness.jobExists(t, meetings.KindTranscribe, "transcribe:meeting:"+created.Meeting.ID+":g1") {
		t.Fatal("a transcribe job was queued without the credits to pay for it")
	}

	harness.grant(t, owner, 2)
	stillShort := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if stillShort.Status != http.StatusPaymentRequired {
		t.Fatalf("two credits paid for three minutes of audio: status %d, body %s", stillShort.Status, stillShort.Body)
	}

	harness.grant(t, owner, 1)
	finalized := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}
	var result finalizePayload
	finalized.decode(t, &result)
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindTranscribe {
		t.Fatalf("queued %+v", result.Queued)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance = %d, want 0: three minutes of audio costs three credits", got)
	}
}

func TestASmallAudioObjectCostsExactlyOneCredit(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signInBroke("one-credit@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Tiny", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")
	harness.grant(t, owner, 1)

	finalized := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize: status %d, body %s", finalized.Status, finalized.Body)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance = %d, want 0: any paid transcribe costs at least one credit", got)
	}
}

func TestAnAbsurdDeclaredDurationIsRejectedAtTheBoundary(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("absurd-duration@example.com")

	for _, duration := range []int64{2147483647, 86401, -1} {
		created := harness.call(http.MethodPost, "/v1/meetings", map[string]any{
			"title":      "Overflowing",
			"source":     "desktop",
			"duration_s": duration,
		}, harness.as(owner)...)
		if created.Status != http.StatusBadRequest {
			t.Fatalf("duration_s %d was accepted: status %d, body %s", duration, created.Status, created.Body)
		}
	}

	accepted := harness.call(http.MethodPost, "/v1/meetings", map[string]any{
		"title":      "A long day",
		"source":     "desktop",
		"duration_s": 86400,
	}, harness.as(owner)...)
	if accepted.Status != http.StatusCreated {
		t.Fatalf("a twenty four hour meeting was refused: status %d, body %s", accepted.Status, accepted.Body)
	}
}

func TestFinalizeChargesAHostedSummary(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signInBroke("summary-credits@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "With a transcript", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")
	harness.memory.Put("transcripts", transcriptKey(owner.Workspace.ID, created.Meeting.ID),
		[]byte(`{"segments":[]}`), "application/json")

	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"

	broke := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if broke.Status != http.StatusPaymentRequired {
		t.Fatalf("a summary was queued for free: status %d, body %s", broke.Status, broke.Body)
	}

	harness.grant(t, owner, 1)
	finalized := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("summary finalize: status %d, body %s", finalized.Status, finalized.Body)
	}
	var result finalizePayload
	finalized.decode(t, &result)
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindSummarise {
		t.Fatalf("queued %+v", result.Queued)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance = %d, want 0: a hosted summary costs at least one credit", got)
	}
}

func TestARefinalizeThatQueuesNothingChargesNothing(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signInBroke("refinalize@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Once only", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")
	harness.grant(t, owner, 1)

	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"
	if got := harness.call(http.MethodPost, path, nil, harness.as(owner)...); got.Status != http.StatusOK {
		t.Fatalf("first finalize: status %d, body %s", got.Status, got.Body)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance after the first finalize = %d, want 0", got)
	}

	again := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if again.Status != http.StatusOK {
		t.Fatalf("re-finalize: status %d, body %s", again.Status, again.Body)
	}
	var result finalizePayload
	again.decode(t, &result)
	if len(result.Queued) != 0 {
		t.Fatalf("re-finalize queued %+v", result.Queued)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance after a re-finalize = %d, want 0: nothing new was queued", got)
	}
}

func TestNothingIsChargedWhenTheEnqueueFails(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signInBroke("enqueue-failure@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Queue is down", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID),
		make([]byte, bytesForMinutes(2)), "audio/opus")
	harness.grant(t, owner, 5)

	harness.failEnqueues(true)
	failed := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if failed.Status != http.StatusInternalServerError {
		t.Fatalf("finalize with a broken queue: status %d, body %s", failed.Status, failed.Body)
	}
	if got := harness.balance(t, owner); got != 5 {
		t.Fatalf("balance after a failed enqueue = %d, want 5: the debit must be refunded", got)
	}

	harness.failEnqueues(false)
	recovered := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if recovered.Status != http.StatusOK {
		t.Fatalf("finalize once the queue recovers: status %d, body %s", recovered.Status, recovered.Body)
	}
	if got := harness.balance(t, owner); got != 3 {
		t.Fatalf("balance after the retry = %d, want 3: two minutes charged once", got)
	}
}

func TestAnEmptyAudioObjectIsRefusedRatherThanQueuedForFree(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("empty-audio@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "Nothing recorded", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), []byte{}, "audio/opus")

	before := harness.balance(t, owner)
	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"

	refused := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if refused.Status != http.StatusUnprocessableEntity {
		t.Fatalf("finalize with an empty audio object: status %d, body %s", refused.Status, refused.Body)
	}
	if code := refused.errorCode(t); code != "empty_object" {
		t.Fatalf("error code = %q, want empty_object", code)
	}
	if harness.jobExists(t, meetings.KindTranscribe, "transcribe:meeting:"+created.Meeting.ID+":g1") {
		t.Fatal("an empty audio object was queued for transcription")
	}
	if got := harness.balance(t, owner); got != before {
		t.Fatalf("balance moved from %d to %d for an empty object", before, got)
	}
	if got := harness.generation(t, created.Meeting.ID); got != 0 {
		t.Fatalf("a refused finalize moved the generation to %d", got)
	}

	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")
	accepted := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if accepted.Status != http.StatusOK {
		t.Fatalf("finalize once the audio is real: status %d, body %s", accepted.Status, accepted.Body)
	}
	if got := harness.balance(t, owner); got != before-1 {
		t.Fatalf("balance = %d, want %d: a real object costs one credit", got, before-1)
	}
}
