//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestAskReturnsCitedAnswer(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner@example.com")

	meeting := harness.createMeeting(owner, map[string]any{"title": "Planning call", "source": "desktop"})
	putSegments(t, harness, owner, meeting.Meeting.ID, []map[string]any{
		{"seq": 0, "speaker": "Ada", "start_s": 5, "end_s": 8, "text": "The launch date is set for March"},
		{"seq": 1, "speaker": "Bo", "start_s": 8, "end_s": 12, "text": "Marketing will prepare the campaign"},
	})

	asked := harness.call(http.MethodPost, "/v1/ask", map[string]any{
		"question": "launch date",
	}, harness.as(owner)...)
	if asked.Status != http.StatusOK {
		t.Fatalf("ask: status %d, body %s", asked.Status, asked.Body)
	}
	var result struct {
		Answer    string `json:"answer"`
		Citations []struct {
			MeetingID string  `json:"meeting_id"`
			StartS    float64 `json:"start_s"`
			Text      string  `json:"text"`
		} `json:"citations"`
	}
	asked.decode(t, &result)
	if result.Answer == "" {
		t.Fatal("expected a non-empty answer")
	}
	if len(result.Citations) == 0 {
		t.Fatal("expected at least one citation")
	}
	if result.Citations[0].MeetingID != meeting.Meeting.ID {
		t.Fatalf("citation points at %s, want %s", result.Citations[0].MeetingID, meeting.Meeting.ID)
	}

	empty := harness.call(http.MethodPost, "/v1/ask", map[string]any{"question": ""}, harness.as(owner)...)
	if empty.Status != http.StatusBadRequest {
		t.Fatalf("empty question should be rejected: status %d", empty.Status)
	}
}
