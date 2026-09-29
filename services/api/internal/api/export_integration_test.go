//go:build integration

package api_test

import (
	"net/http"
	"testing"
)

func TestExportMeetingDeliversToProvider(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Quarterly review",
		"source":     "desktop",
		"visibility": "workspace",
	})
	path := "/v1/meetings/" + created.Meeting.ID + "/export"

	response := harness.call(http.MethodPost, path, map[string]any{"target": "slack"}, harness.as(owner)...)
	if response.Status != http.StatusAccepted {
		t.Fatalf("export: status %d, body %s", response.Status, response.Body)
	}

	delivered := harness.slackFake.Delivered()
	if len(delivered) != 1 {
		t.Fatalf("expected one delivery, got %d", len(delivered))
	}
	if delivered[0].Title != "Quarterly review" {
		t.Fatalf("unexpected title: %q", delivered[0].Title)
	}
	if delivered[0].MeetingID.String() != created.Meeting.ID {
		t.Fatalf("unexpected meeting id: %s", delivered[0].MeetingID)
	}
}

func TestExportMeetingRejectsUnknownProvider(t *testing.T) {
	harness := newLibraryHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.createMeeting(owner, map[string]any{
		"title":      "Sync",
		"source":     "desktop",
		"visibility": "workspace",
	})
	path := "/v1/meetings/" + created.Meeting.ID + "/export"

	response := harness.call(http.MethodPost, path, map[string]any{"target": "telegram"}, harness.as(owner)...)
	if response.Status != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body %s", response.Status, response.Body)
	}
}
