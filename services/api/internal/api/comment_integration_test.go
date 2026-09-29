//go:build integration

package api_test

import (
	"net/http"
	"testing"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

func TestCommentsLifecycle(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner@example.com")

	meeting := harness.createMeeting(owner, map[string]any{"title": "Retro", "source": "desktop"})

	created := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/comments", map[string]any{
		"at_s": 12.5,
		"body": "Great point about caching",
	}, harness.as(owner)...)
	if created.Status != http.StatusCreated {
		t.Fatalf("create comment: status %d, body %s", created.Status, created.Body)
	}
	var comment struct {
		ID        string  `json:"id"`
		MeetingID string  `json:"meeting_id"`
		AtS       float64 `json:"at_s"`
		Body      string  `json:"body"`
		UserID    string  `json:"user_id"`
	}
	created.decode(t, &comment)
	if comment.AtS != 12.5 || comment.Body != "Great point about caching" {
		t.Fatalf("unexpected comment: %+v", comment)
	}
	if comment.UserID == "" {
		t.Fatal("expected comment to record its author")
	}

	blank := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/comments", map[string]any{
		"at_s": 1, "body": "   ",
	}, harness.as(owner)...)
	if blank.Status != http.StatusBadRequest {
		t.Fatalf("blank comment should be rejected: status %d", blank.Status)
	}

	listed := harness.call(http.MethodGet, "/v1/meetings/"+meeting.Meeting.ID+"/comments", nil, harness.as(owner)...)
	var list struct {
		Comments []struct {
			ID string `json:"id"`
		} `json:"comments"`
	}
	listed.decode(t, &list)
	if len(list.Comments) != 1 || list.Comments[0].ID != comment.ID {
		t.Fatalf("expected the one comment we created, got %+v", list.Comments)
	}

	updated := harness.call(http.MethodPatch, "/v1/meetings/"+meeting.Meeting.ID+"/comments/"+comment.ID, map[string]any{
		"body": "Great point about cache invalidation",
	}, harness.as(owner)...)
	if updated.Status != http.StatusOK {
		t.Fatalf("update comment: status %d, body %s", updated.Status, updated.Body)
	}
	var afterUpdate struct {
		Body string `json:"body"`
	}
	updated.decode(t, &afterUpdate)
	if afterUpdate.Body != "Great point about cache invalidation" {
		t.Fatalf("comment not updated: %+v", afterUpdate)
	}

	removed := harness.call(http.MethodDelete, "/v1/meetings/"+meeting.Meeting.ID+"/comments/"+comment.ID, nil, harness.as(owner)...)
	if removed.Status != http.StatusNoContent {
		t.Fatalf("delete comment: status %d, body %s", removed.Status, removed.Body)
	}

	empty := harness.call(http.MethodGet, "/v1/meetings/"+meeting.Meeting.ID+"/comments", nil, harness.as(owner)...)
	var afterDelete struct {
		Comments []struct{} `json:"comments"`
	}
	empty.decode(t, &afterDelete)
	if len(afterDelete.Comments) != 0 {
		t.Fatalf("expected no comments after delete, got %d", len(afterDelete.Comments))
	}
}

func TestCommentAuthorRestrictionsAndIsolation(t *testing.T) {
	memory := storage.NewMemory()
	pool := dbtest.New(t)
	harness := buildLibraryHarness(t, pool, memory, memory)
	owner := harness.signIn("owner2@example.com")
	member := harness.join(t, owner, "member@example.com", auth.RoleMember)

	meeting := harness.createMeeting(owner, map[string]any{"title": "Planning", "source": "desktop", "visibility": "workspace"})

	memberComment := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/comments", map[string]any{
		"at_s": 3, "body": "Member observation",
	}, harness.as(member)...)
	if memberComment.Status != http.StatusCreated {
		t.Fatalf("member create comment: status %d, body %s", memberComment.Status, memberComment.Body)
	}
	var mc struct {
		ID string `json:"id"`
	}
	memberComment.decode(t, &mc)

	ownerComment := harness.call(http.MethodPost, "/v1/meetings/"+meeting.Meeting.ID+"/comments", map[string]any{
		"at_s": 4, "body": "Owner observation",
	}, harness.as(owner)...)
	var oc struct {
		ID string `json:"id"`
	}
	ownerComment.decode(t, &oc)

	forbidden := harness.call(http.MethodPatch, "/v1/meetings/"+meeting.Meeting.ID+"/comments/"+oc.ID, map[string]any{
		"body": "member editing owner comment",
	}, harness.as(member)...)
	if forbidden.Status != http.StatusForbidden {
		t.Fatalf("member editing owner comment should be forbidden: status %d, body %s", forbidden.Status, forbidden.Body)
	}

	ownerDeletesMember := harness.call(http.MethodDelete, "/v1/meetings/"+meeting.Meeting.ID+"/comments/"+mc.ID, nil, harness.as(owner)...)
	if ownerDeletesMember.Status != http.StatusNoContent {
		t.Fatalf("owner deleting member comment should succeed: status %d, body %s", ownerDeletesMember.Status, ownerDeletesMember.Body)
	}

	other := harness.signIn("outsider@example.com")
	leak := harness.call(http.MethodGet, "/v1/meetings/"+meeting.Meeting.ID+"/comments", nil, harness.as(other)...)
	if leak.Status != http.StatusNotFound {
		t.Fatalf("outsider should not see the meeting: status %d, body %s", leak.Status, leak.Body)
	}
}
