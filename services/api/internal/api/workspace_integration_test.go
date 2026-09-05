//go:build integration

package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type workspacePayload struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	BotName       string `json:"bot_name"`
	RetentionDays int32  `json:"retention_days"`
	Role          string `json:"role"`
}

type invitePayload struct {
	ID        string `json:"id"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	Token     string `json:"token"`
	AcceptURL string `json:"accept_url"`
	ExpiresAt string `json:"expires_at"`
}

type membersPayload struct {
	Members []struct {
		UserID string `json:"user_id"`
		Email  string `json:"email"`
		Role   string `json:"role"`
	} `json:"members"`
}

func TestCreateAndListWorkspaces(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")

	created := harness.do(http.MethodPost, "/v1/workspaces", map[string]string{
		"name": "Acme Research",
	}, withBearer(owner.AccessToken))
	if created.Status != http.StatusCreated {
		t.Fatalf("create: status %d, body %s", created.Status, created.Body)
	}
	var workspace workspacePayload
	created.decode(t, &workspace)
	if workspace.Slug != "acme-research" {
		t.Fatalf("slug %q", workspace.Slug)
	}
	if workspace.Role != string(auth.RoleOwner) || workspace.RetentionDays != 365 || workspace.BotName == "" {
		t.Fatalf("workspace %+v", workspace)
	}

	duplicate := harness.do(http.MethodPost, "/v1/workspaces", map[string]string{
		"name": "Acme Research", "slug": "acme-research",
	}, withBearer(owner.AccessToken))
	if duplicate.Status != http.StatusConflict {
		t.Fatalf("duplicate slug: status %d, body %s", duplicate.Status, duplicate.Body)
	}

	invalid := harness.do(http.MethodPost, "/v1/workspaces", map[string]string{"name": "  "}, withBearer(owner.AccessToken))
	if invalid.Status != http.StatusBadRequest {
		t.Fatalf("blank name: status %d", invalid.Status)
	}

	badSlug := harness.do(http.MethodPost, "/v1/workspaces", map[string]string{
		"name": "Fine", "slug": "Not A Slug",
	}, withBearer(owner.AccessToken))
	if badSlug.Status != http.StatusBadRequest {
		t.Fatalf("bad slug: status %d", badSlug.Status)
	}

	listed := harness.do(http.MethodGet, "/v1/workspaces", nil, withBearer(owner.AccessToken))
	if listed.Status != http.StatusOK {
		t.Fatalf("list: status %d", listed.Status)
	}
	var list struct {
		Workspaces []workspacePayload `json:"workspaces"`
	}
	listed.decode(t, &list)
	if len(list.Workspaces) != 2 {
		t.Fatalf("listed %d workspaces, want 2", len(list.Workspaces))
	}
}

func TestGetWorkspaceAnswersWithAndWithoutATrailingSlash(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")

	for _, path := range []string{"/v1/workspaces/" + owner.Workspace.ID, "/v1/workspaces/" + owner.Workspace.ID + "/"} {
		got := harness.do(http.MethodGet, path, nil, withBearer(owner.AccessToken))
		if got.Status != http.StatusOK {
			t.Fatalf("%s: status %d, body %s", path, got.Status, got.Body)
		}
		var workspace workspacePayload
		got.decode(t, &workspace)
		if workspace.ID != owner.Workspace.ID || workspace.Role != "owner" {
			t.Fatalf("%s: workspace %+v", path, workspace)
		}
	}
}

func TestCrossWorkspaceAccessIsForbiddenAndDoesNotLeakExistence(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	stranger := harness.signIn("stranger@example.com")

	existing := harness.do(http.MethodGet, "/v1/workspaces/"+owner.Workspace.ID+"/members", nil,
		withBearer(stranger.AccessToken))
	if existing.Status != http.StatusForbidden {
		t.Fatalf("existing workspace: status %d, body %s", existing.Status, existing.Body)
	}
	if got := existing.errorCode(t); got != httpx.CodeForbidden {
		t.Fatalf("code %q", got)
	}

	missing := harness.do(http.MethodGet, "/v1/workspaces/"+uuid.NewString()+"/members", nil,
		withBearer(stranger.AccessToken))
	if missing.Status != http.StatusForbidden {
		t.Fatalf("missing workspace: status %d", missing.Status)
	}
	if string(existing.Body) != string(missing.Body) {
		t.Fatalf("an existing workspace answers %q and a missing one %q", existing.Body, missing.Body)
	}

	header := harness.do(http.MethodGet, "/v1/me", nil,
		withBearer(stranger.AccessToken), withWorkspace(owner.Workspace.ID))
	if header.Status != http.StatusOK {
		t.Fatalf("the workspace header must not affect /v1/me: status %d", header.Status)
	}

	malformed := harness.do(http.MethodGet, "/v1/workspaces/not-a-uuid/members", nil, withBearer(stranger.AccessToken))
	if malformed.Status != http.StatusBadRequest {
		t.Fatalf("malformed workspace id: status %d, body %s", malformed.Status, malformed.Body)
	}
}

func TestInviteFlowAndRoleRules(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.signIn("member@example.com")

	invited := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "Member@Example.com",
		"role":  "member",
	}, withBearer(owner.AccessToken))
	if invited.Status != http.StatusCreated {
		t.Fatalf("invite: status %d, body %s", invited.Status, invited.Body)
	}
	var invite invitePayload
	invited.decode(t, &invite)
	if invite.Token == "" || invite.Email != "member@example.com" || invite.Role != "member" {
		t.Fatalf("invite %+v", invite)
	}
	if !strings.HasSuffix(invite.AcceptURL, invite.Token) {
		t.Fatalf("accept url %q", invite.AcceptURL)
	}

	inviteEmail := harness.sender.lastEmail(t)
	if inviteEmail.To != "member@example.com" || !strings.Contains(inviteEmail.Text, invite.Token) {
		t.Fatalf("invite email %+v", inviteEmail)
	}

	duplicate := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "member@example.com",
	}, withBearer(owner.AccessToken))
	if duplicate.Status != http.StatusConflict {
		t.Fatalf("duplicate invite: status %d, body %s", duplicate.Status, duplicate.Body)
	}

	stranger := harness.signIn("stranger@example.com")
	mismatched := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil, withBearer(stranger.AccessToken))
	if mismatched.Status != http.StatusForbidden {
		t.Fatalf("mismatched invite: status %d, body %s", mismatched.Status, mismatched.Body)
	}

	unknown := harness.do(http.MethodPost, "/v1/invites/no-such-token/accept", nil, withBearer(member.AccessToken))
	if unknown.Status != http.StatusNotFound {
		t.Fatalf("unknown invite: status %d", unknown.Status)
	}

	accepted := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil, withBearer(member.AccessToken))
	if accepted.Status != http.StatusOK {
		t.Fatalf("accept: status %d, body %s", accepted.Status, accepted.Body)
	}
	var acceptedBody struct {
		Workspace workspacePayload `json:"workspace"`
	}
	accepted.decode(t, &acceptedBody)
	if acceptedBody.Workspace.ID != owner.Workspace.ID || acceptedBody.Workspace.Role != "member" {
		t.Fatalf("accepted %+v", acceptedBody.Workspace)
	}

	replayed := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil, withBearer(member.AccessToken))
	if replayed.Status != http.StatusConflict && replayed.Status != http.StatusForbidden {
		t.Fatalf("replayed invite: status %d, body %s", replayed.Status, replayed.Body)
	}

	members := harness.do(http.MethodGet, "/v1/workspaces/"+owner.Workspace.ID+"/members", nil, withBearer(member.AccessToken))
	if members.Status != http.StatusOK {
		t.Fatalf("members: status %d, body %s", members.Status, members.Body)
	}
	var listed membersPayload
	members.decode(t, &listed)
	if len(listed.Members) != 2 {
		t.Fatalf("listed %d members, want 2", len(listed.Members))
	}

	forbidden := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "someone@example.com",
	}, withBearer(member.AccessToken))
	if forbidden.Status != http.StatusForbidden {
		t.Fatalf("a member could invite: status %d, body %s", forbidden.Status, forbidden.Body)
	}

	promoted := harness.do(http.MethodPatch, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+member.User.ID,
		map[string]string{"role": "admin"}, withBearer(owner.AccessToken))
	if promoted.Status != http.StatusOK {
		t.Fatalf("promote: status %d, body %s", promoted.Status, promoted.Body)
	}

	memberAfterPromotion := harness.signIn("member@example.com")
	demoteOwner := harness.do(http.MethodPatch, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+owner.User.ID,
		map[string]string{"role": "member"}, withBearer(memberAfterPromotion.AccessToken), withWorkspace(owner.Workspace.ID))
	if demoteOwner.Status != http.StatusForbidden {
		t.Fatalf("an admin demoted an owner: status %d, body %s", demoteOwner.Status, demoteOwner.Body)
	}

	promoteToOwner := harness.do(http.MethodPatch, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+stranger.User.ID,
		map[string]string{"role": "owner"}, withBearer(memberAfterPromotion.AccessToken), withWorkspace(owner.Workspace.ID))
	if promoteToOwner.Status != http.StatusNotFound && promoteToOwner.Status != http.StatusForbidden {
		t.Fatalf("promotion to owner by an admin: status %d, body %s", promoteToOwner.Status, promoteToOwner.Body)
	}

	selfDemotion := harness.do(http.MethodPatch, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+owner.User.ID,
		map[string]string{"role": "admin"}, withBearer(owner.AccessToken))
	if selfDemotion.Status != http.StatusForbidden {
		t.Fatalf("self demotion: status %d, body %s", selfDemotion.Status, selfDemotion.Body)
	}

	badRole := harness.do(http.MethodPatch, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+member.User.ID,
		map[string]string{"role": "root"}, withBearer(owner.AccessToken))
	if badRole.Status != http.StatusBadRequest {
		t.Fatalf("bad role: status %d", badRole.Status)
	}

	removedSelf := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+owner.User.ID,
		nil, withBearer(owner.AccessToken))
	if removedSelf.Status != http.StatusForbidden {
		t.Fatalf("the sole owner removed themselves: status %d, body %s", removedSelf.Status, removedSelf.Body)
	}

	removed := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+member.User.ID,
		nil, withBearer(owner.AccessToken))
	if removed.Status != http.StatusNoContent {
		t.Fatalf("remove: status %d, body %s", removed.Status, removed.Body)
	}

	afterRemoval := harness.do(http.MethodGet, "/v1/workspaces/"+owner.Workspace.ID+"/members", nil,
		withBearer(memberAfterPromotion.AccessToken), withWorkspace(owner.Workspace.ID))
	if afterRemoval.Status != http.StatusForbidden {
		t.Fatalf("a removed member still had access: status %d", afterRemoval.Status)
	}

	missingMember := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/members/"+uuid.NewString(),
		nil, withBearer(owner.AccessToken))
	if missingMember.Status != http.StatusNotFound {
		t.Fatalf("missing member: status %d", missingMember.Status)
	}
}

func TestInviteRequiresAnAdminAndRejectsBadInput(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")

	badEmail := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "not-an-email",
	}, withBearer(owner.AccessToken))
	if badEmail.Status != http.StatusBadRequest {
		t.Fatalf("bad email: status %d, body %s", badEmail.Status, badEmail.Body)
	}

	badRole := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "someone@example.com", "role": "root",
	}, withBearer(owner.AccessToken))
	if badRole.Status != http.StatusBadRequest {
		t.Fatalf("bad role: status %d", badRole.Status)
	}

	selfInvite := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "owner@example.com",
	}, withBearer(owner.AccessToken))
	if selfInvite.Status != http.StatusConflict {
		t.Fatalf("inviting an existing member: status %d, body %s", selfInvite.Status, selfInvite.Body)
	}

	listed := harness.do(http.MethodGet, "/v1/workspaces/"+owner.Workspace.ID+"/invites", nil, withBearer(owner.AccessToken))
	if listed.Status != http.StatusOK {
		t.Fatalf("list invites: status %d", listed.Status)
	}
	if strings.Contains(string(listed.Body), "token") {
		t.Fatalf("the invite listing leaked a token: %s", listed.Body)
	}
}

func TestWorkspaceRoutesUseTheDatabaseRoleNotTheToken(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.signIn("member@example.com")

	invited := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "member@example.com",
	}, withBearer(owner.AccessToken))
	var invite invitePayload
	invited.decode(t, &invite)

	if accepted := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil,
		withBearer(member.AccessToken)); accepted.Status != http.StatusOK {
		t.Fatalf("accept: status %d, body %s", accepted.Status, accepted.Body)
	}

	stale := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "someone@example.com",
	}, withBearer(member.AccessToken))
	if stale.Status != http.StatusForbidden {
		t.Fatalf("the owner claim in the token was trusted: status %d, body %s", stale.Status, stale.Body)
	}
}

func TestRevokedInvitesCannotBeAccepted(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.signIn("member@example.com")

	invited := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "member@example.com",
	}, withBearer(owner.AccessToken))
	if invited.Status != http.StatusCreated {
		t.Fatalf("invite: status %d, body %s", invited.Status, invited.Body)
	}
	var invite invitePayload
	invited.decode(t, &invite)

	revoked := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/invites/"+invite.ID,
		nil, withBearer(owner.AccessToken))
	if revoked.Status != http.StatusNoContent {
		t.Fatalf("revoke: status %d, body %s", revoked.Status, revoked.Body)
	}

	accepted := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil, withBearer(member.AccessToken))
	if accepted.Status != http.StatusNotFound {
		t.Fatalf("a revoked invite was accepted: status %d, body %s", accepted.Status, accepted.Body)
	}

	again := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/invites/"+invite.ID,
		nil, withBearer(owner.AccessToken))
	if again.Status != http.StatusNotFound {
		t.Fatalf("revoking twice: status %d", again.Status)
	}

	replacement := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "member@example.com",
	}, withBearer(owner.AccessToken))
	if replacement.Status != http.StatusCreated {
		t.Fatalf("re-invite after a revoke: status %d, body %s", replacement.Status, replacement.Body)
	}

	forbidden := harness.do(http.MethodDelete, "/v1/workspaces/"+owner.Workspace.ID+"/invites/"+invite.ID,
		nil, withBearer(member.AccessToken), withWorkspace(owner.Workspace.ID))
	if forbidden.Status != http.StatusForbidden {
		t.Fatalf("a non-member revoked an invite: status %d", forbidden.Status)
	}
}

func TestTheDefaultWorkspaceIsTheOldestMembership(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	member := harness.signIn("member@example.com")

	invited := harness.do(http.MethodPost, "/v1/workspaces/"+owner.Workspace.ID+"/invites", map[string]string{
		"email": "member@example.com",
	}, withBearer(owner.AccessToken))
	var invite invitePayload
	invited.decode(t, &invite)

	if accepted := harness.do(http.MethodPost, "/v1/invites/"+invite.Token+"/accept", nil,
		withBearer(member.AccessToken)); accepted.Status != http.StatusOK {
		t.Fatalf("accept: status %d, body %s", accepted.Status, accepted.Body)
	}

	refreshed := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": member.RefreshToken,
	})
	if refreshed.Status != http.StatusOK {
		t.Fatalf("refresh: status %d, body %s", refreshed.Status, refreshed.Body)
	}
	var renewed session
	refreshed.decode(t, &renewed)
	if renewed.Workspace.ID != member.Workspace.ID {
		t.Fatalf("refresh moved the default workspace to %s, want %s", renewed.Workspace.ID, member.Workspace.ID)
	}
	if renewed.Workspace.Role != "owner" {
		t.Fatalf("default workspace role %q, want owner", renewed.Workspace.Role)
	}

	me := harness.do(http.MethodGet, "/v1/me", nil, withBearer(renewed.AccessToken))
	if me.Status != http.StatusOK {
		t.Fatalf("me: status %d, body %s", me.Status, me.Body)
	}
	var body struct {
		Workspaces []workspacePayload `json:"workspaces"`
	}
	me.decode(t, &body)
	if len(body.Workspaces) != 2 {
		t.Fatalf("workspaces %+v", body.Workspaces)
	}
	if body.Workspaces[0].ID != member.Workspace.ID {
		t.Fatalf("me listed %s first, want the personal workspace %s", body.Workspaces[0].ID, member.Workspace.ID)
	}
}
