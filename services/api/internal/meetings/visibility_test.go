package meetings_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/meetings"
)

func TestVisibilityMatrix(t *testing.T) {
	workspace := uuid.New()
	other := uuid.New()
	owner := uuid.New()
	member := uuid.New()

	ownerActor := auth.Membership{WorkspaceID: workspace, UserID: owner, Role: auth.RoleMember}
	memberActor := auth.Membership{WorkspaceID: workspace, UserID: member, Role: auth.RoleMember}
	adminActor := auth.Membership{WorkspaceID: workspace, UserID: member, Role: auth.RoleAdmin}
	strangerActor := auth.Membership{WorkspaceID: other, UserID: member, Role: auth.RoleOwner}

	cases := []struct {
		visibility string
		actor      auth.Membership
		name       string
		view       bool
		manage     bool
	}{
		{meetings.VisibilityPrivate, ownerActor, "owner", true, true},
		{meetings.VisibilityPrivate, memberActor, "member", false, false},
		{meetings.VisibilityPrivate, adminActor, "admin", false, false},
		{meetings.VisibilityPrivate, strangerActor, "stranger", false, false},
		{meetings.VisibilityWorkspace, ownerActor, "owner", true, true},
		{meetings.VisibilityWorkspace, memberActor, "member", true, false},
		{meetings.VisibilityWorkspace, adminActor, "admin", true, true},
		{meetings.VisibilityWorkspace, strangerActor, "stranger", false, false},
		{meetings.VisibilityLink, ownerActor, "owner", true, true},
		{meetings.VisibilityLink, memberActor, "member", true, false},
		{meetings.VisibilityLink, adminActor, "admin", true, true},
		{meetings.VisibilityLink, strangerActor, "stranger", false, false},
	}

	for _, testCase := range cases {
		t.Run(testCase.visibility+"/"+testCase.name, func(t *testing.T) {
			meeting := meetings.Meeting{WorkspaceID: workspace, OwnerUserID: &owner, Visibility: testCase.visibility}
			if got := meeting.VisibleTo(testCase.actor); got != testCase.view {
				t.Fatalf("VisibleTo = %v, want %v", got, testCase.view)
			}
			if got := meeting.ManageableBy(testCase.actor); got != testCase.manage {
				t.Fatalf("ManageableBy = %v, want %v", got, testCase.manage)
			}
		})
	}
}

func TestAnOwnerlessMeetingIsNeverPrivatelyVisible(t *testing.T) {
	workspace := uuid.New()
	actor := auth.Membership{WorkspaceID: workspace, UserID: uuid.New(), Role: auth.RoleOwner}
	meeting := meetings.Meeting{WorkspaceID: workspace, Visibility: meetings.VisibilityPrivate}

	if meeting.VisibleTo(actor) || meeting.ManageableBy(actor) {
		t.Fatal("a private meeting with no owner was reachable")
	}
}

func TestAnUnknownVisibilityFallsBackToPrivate(t *testing.T) {
	workspace := uuid.New()
	owner := uuid.New()
	actor := auth.Membership{WorkspaceID: workspace, UserID: uuid.New(), Role: auth.RoleOwner}
	meeting := meetings.Meeting{WorkspaceID: workspace, OwnerUserID: &owner, Visibility: "public"}

	if meeting.VisibleTo(actor) {
		t.Fatal("an unrecognised visibility was treated as open")
	}
}
