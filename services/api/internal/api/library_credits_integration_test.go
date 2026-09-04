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

func TestFinalizeChargesCloudTranscriptionOnce(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("transcribe-credits@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Fifteen minutes",
		"source":     "desktop",
		"duration_s": 900,
	})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")

	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"
	transcribeKey := "transcribe:meeting:" + created.Meeting.ID + ":g1"

	broke := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if broke.Status != http.StatusPaymentRequired {
		t.Fatalf("finalize on a zero balance: status %d, body %s", broke.Status, broke.Body)
	}
	var refused entitlementPayload
	broke.decode(t, &refused)
	if refused.Error.Code != "insufficient_credits" {
		t.Fatalf("error code = %q, want insufficient_credits", refused.Error.Code)
	}
	if refused.Error.TopUpURL != "http://localhost:3000/billing/top-up" {
		t.Fatalf("top_up_url = %q", refused.Error.TopUpURL)
	}
	if harness.jobExists(t, meetings.KindTranscribe, transcribeKey) {
		t.Fatal("a transcribe job was queued without the credits to pay for it")
	}
	if got := harness.generation(t, created.Meeting.ID); got != 0 {
		t.Fatalf("a refused finalize moved the generation to %d", got)
	}

	harness.grant(t, owner, 14)
	stillShort := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if stillShort.Status != http.StatusPaymentRequired {
		t.Fatalf("finalize one minute short: status %d, body %s", stillShort.Status, stillShort.Body)
	}

	harness.grant(t, owner, 1)
	finalized := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize at exactly the price: status %d, body %s", finalized.Status, finalized.Body)
	}
	var result finalizePayload
	finalized.decode(t, &result)
	if len(result.Queued) != 1 || result.Queued[0] != meetings.KindTranscribe {
		t.Fatalf("queued %+v", result.Queued)
	}
	if !harness.jobExists(t, meetings.KindTranscribe, transcribeKey) {
		t.Fatal("no transcribe job was enqueued")
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance after finalize = %d, want 0: fifteen minutes of audio costs fifteen credits", got)
	}

	again := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if again.Status != http.StatusOK {
		t.Fatalf("re-finalize: status %d, body %s", again.Status, again.Body)
	}
	again.decode(t, &result)
	if len(result.Queued) != 0 {
		t.Fatalf("re-finalize queued %+v", result.Queued)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance after a re-finalize = %d, want 0: nothing new was queued", got)
	}
}

func TestFinalizeChargesHostedSummariesAtATenthOfTheMinutes(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("summary-credits@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Fifteen minutes with a transcript",
		"source":     "desktop",
		"duration_s": 900,
	})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")
	harness.memory.Put("transcripts", transcriptKey(owner.Workspace.ID, created.Meeting.ID), []byte(`{"segments":[]}`), "application/json")

	path := "/v1/meetings/" + created.Meeting.ID + "/finalize"

	harness.grant(t, owner, 1)
	short := harness.call(http.MethodPost, path, nil, harness.as(owner)...)
	if short.Status != http.StatusPaymentRequired {
		t.Fatalf("summary finalize below the price: status %d, body %s", short.Status, short.Body)
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
		t.Fatalf("balance after a summary finalize = %d, want 0: fifteen transcript minutes cost two credits", got)
	}
}

func TestFinalizeIsFreeWhenTheDurationIsUnknown(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("unknown-duration@example.com")

	created := harness.createMeeting(owner, map[string]any{"title": "No duration", "source": "desktop"})
	harness.memory.Put("audio", audioKey(owner.Workspace.ID, created.Meeting.ID), make([]byte, 2048), "audio/opus")

	finalized := harness.call(http.MethodPost, "/v1/meetings/"+created.Meeting.ID+"/finalize", nil, harness.as(owner)...)
	if finalized.Status != http.StatusOK {
		t.Fatalf("finalize with an unknown duration: status %d, body %s", finalized.Status, finalized.Body)
	}
	if got := harness.balance(t, owner); got != 0 {
		t.Fatalf("balance = %d, want 0", got)
	}
}
