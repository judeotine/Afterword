//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestSearchFindsSegmentsByKeyword(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner@example.com")

	first := harness.createMeeting(owner, map[string]any{"title": "Budget review", "source": "desktop"})
	second := harness.createMeeting(owner, map[string]any{"title": "Design sync", "source": "desktop"})

	putSegments(t, harness, owner, first.Meeting.ID, []map[string]any{
		{"seq": 0, "speaker": "Ada", "start_s": 0, "end_s": 2, "text": "We must reduce the quarterly budget significantly"},
		{"seq": 1, "speaker": "Bo", "start_s": 2, "end_s": 4, "text": "Marketing spend is the main driver"},
	})
	putSegments(t, harness, owner, second.Meeting.ID, []map[string]any{
		{"seq": 0, "speaker": "Ada", "start_s": 0, "end_s": 2, "text": "The new landing page needs a hero image"},
	})

	results := harness.call(http.MethodGet, "/v1/search?q=budget", nil, harness.as(owner)...)
	if results.Status != http.StatusOK {
		t.Fatalf("search: status %d, body %s", results.Status, results.Body)
	}
	var payload struct {
		Query string `json:"query"`
		Hits  []struct {
			MeetingID string  `json:"meeting_id"`
			Text      string  `json:"text"`
			Rank      float32 `json:"rank"`
		} `json:"hits"`
	}
	results.decode(t, &payload)
	if payload.Query != "budget" {
		t.Fatalf("echoed query %q", payload.Query)
	}
	if len(payload.Hits) != 1 {
		t.Fatalf("expected 1 hit for budget, got %d: %+v", len(payload.Hits), payload.Hits)
	}
	if payload.Hits[0].MeetingID != first.Meeting.ID {
		t.Fatalf("hit points at meeting %s, want %s", payload.Hits[0].MeetingID, first.Meeting.ID)
	}
	if payload.Hits[0].Rank <= 0 {
		t.Fatalf("expected a positive rank, got %v", payload.Hits[0].Rank)
	}

	empty := harness.call(http.MethodGet, "/v1/search?q=nonexistentword", nil, harness.as(owner)...)
	var emptyPayload struct {
		Hits []struct{} `json:"hits"`
	}
	empty.decode(t, &emptyPayload)
	if len(emptyPayload.Hits) != 0 {
		t.Fatalf("expected no hits, got %d", len(emptyPayload.Hits))
	}

	missing := harness.call(http.MethodGet, "/v1/search", nil, harness.as(owner)...)
	if missing.Status != http.StatusBadRequest {
		t.Fatalf("search without q: status %d, body %s", missing.Status, missing.Body)
	}
}

func putSegments(t *testing.T, harness *libraryHarness, owner session, meetingID string, segments []map[string]any) {
	t.Helper()
	stored := harness.call(http.MethodPut, "/v1/meetings/"+meetingID+"/segments", map[string]any{
		"segments": segments,
	}, harness.as(owner)...)
	if stored.Status != http.StatusOK {
		t.Fatalf("store segments: status %d, body %s", stored.Status, stored.Body)
	}
}
