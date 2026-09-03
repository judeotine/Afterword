//go:build integration

package accounts_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

func newService(t *testing.T) *accounts.Service {
	t.Helper()
	service, err := accounts.NewService(dbtest.New(t))
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	return service
}

func TestEnsureAccountCreatesAUserAndPersonalWorkspaceOnce(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	first, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if err != nil {
		t.Fatalf("ensure account: %v", err)
	}
	if !first.Created {
		t.Fatal("the first sign-in did not create the user")
	}
	if first.Role != auth.RoleOwner {
		t.Fatalf("role %q, want owner", first.Role)
	}
	if first.Workspace.Slug != "person-s-workspace" {
		t.Fatalf("slug %q", first.Workspace.Slug)
	}
	if first.Workspace.RetentionDays != 365 || first.Workspace.BotName == "" {
		t.Fatalf("workspace %+v", first.Workspace)
	}

	second, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if err != nil {
		t.Fatalf("ensure account again: %v", err)
	}
	if second.Created {
		t.Fatal("the second sign-in created another user")
	}
	if second.User.ID != first.User.ID || second.Workspace.ID != first.Workspace.ID {
		t.Fatalf("second %+v, first %+v", second, first)
	}
}

func TestEnsureAccountIsSafeUnderConcurrency(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	const racers = 6
	var wg sync.WaitGroup
	results := make([]accounts.SignIn, racers)
	failures := make([]error, racers)

	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(index int) {
			defer wg.Done()
			signIn, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
				Channel:     auth.ChannelEmail,
				Destination: "person@example.com",
			})
			results[index] = signIn
			failures[index] = err
		}(i)
	}
	wg.Wait()

	users := map[uuid.UUID]bool{}
	for i, err := range failures {
		if err != nil {
			t.Fatalf("racer %d: %v", i, err)
		}
		users[results[i].User.ID] = true
	}
	if len(users) != 1 {
		t.Fatalf("%d distinct users were created", len(users))
	}
}

func TestEnsureAccountKeepsPhoneAndEmailSeparate(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	byEmail, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if err != nil {
		t.Fatalf("ensure by email: %v", err)
	}
	byPhone, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel:     auth.ChannelPhone,
		Destination: "+256700000000",
	})
	if err != nil {
		t.Fatalf("ensure by phone: %v", err)
	}
	if byEmail.User.ID == byPhone.User.ID {
		t.Fatal("a phone sign-in reused the email account")
	}
	if byPhone.Workspace.Slug == byEmail.Workspace.Slug {
		t.Fatalf("both workspaces took the slug %q", byPhone.Workspace.Slug)
	}
}

func TestEnsureAccountForGoogleLinksTheVerifiedEmail(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	byCode, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	})
	if err != nil {
		t.Fatalf("ensure by code: %v", err)
	}

	byGoogle, err := service.EnsureAccountForGoogle(ctx, auth.GoogleProfile{
		Email:         "Person@Example.com",
		EmailVerified: true,
		Name:          "A Person",
	})
	if err != nil {
		t.Fatalf("ensure by google: %v", err)
	}
	if byGoogle.User.ID != byCode.User.ID {
		t.Fatal("google sign-in created a second account for the same address")
	}
	if byGoogle.User.Name != "A Person" {
		t.Fatalf("name %q was not filled in", byGoogle.User.Name)
	}
}

func TestPersonalWorkspaceSlugsDoNotCollide(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	first, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "person@example.com",
	})
	if err != nil {
		t.Fatalf("ensure first: %v", err)
	}
	second, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "person@other.example.com",
	})
	if err != nil {
		t.Fatalf("ensure second: %v", err)
	}
	if first.Workspace.Slug == second.Workspace.Slug {
		t.Fatalf("both workspaces took the slug %q", first.Workspace.Slug)
	}
}

func TestLoadMembershipReportsNonMembers(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	owner, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("ensure owner: %v", err)
	}
	stranger, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "stranger@example.com",
	})
	if err != nil {
		t.Fatalf("ensure stranger: %v", err)
	}

	membership, err := service.LoadMembership(ctx, owner.Workspace.ID, owner.User.ID)
	if err != nil {
		t.Fatalf("load membership: %v", err)
	}
	if membership.Role != auth.RoleOwner {
		t.Fatalf("role %q", membership.Role)
	}

	if _, err := service.LoadMembership(ctx, owner.Workspace.ID, stranger.User.ID); !errors.Is(err, auth.ErrNotMember) {
		t.Fatalf("got %v, want ErrNotMember", err)
	}
	if _, err := service.LoadMembership(ctx, uuid.New(), owner.User.ID); !errors.Is(err, auth.ErrNotMember) {
		t.Fatalf("unknown workspace: got %v, want ErrNotMember", err)
	}
}

func TestOwnerRulesProtectTheLastOwner(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	owner, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("ensure owner: %v", err)
	}
	invitee, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "member@example.com",
	})
	if err != nil {
		t.Fatalf("ensure member: %v", err)
	}

	actor := auth.Membership{WorkspaceID: owner.Workspace.ID, UserID: owner.User.ID, Role: auth.RoleOwner}

	created, err := service.CreateInvite(ctx, accounts.CreateInviteParams{
		Actor: actor,
		Email: "member@example.com",
		Role:  auth.RoleMember,
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if created.Token == "" {
		t.Fatal("no invite token was returned")
	}

	if _, err := service.AcceptInvite(ctx, created.Token, invitee.User); err != nil {
		t.Fatalf("accept invite: %v", err)
	}
	if _, err := service.AcceptInvite(ctx, created.Token, invitee.User); !errors.Is(err, accounts.ErrInviteAlreadyUsed) {
		t.Fatalf("second accept: got %v, want ErrInviteAlreadyUsed", err)
	}

	if err := service.RemoveMember(ctx, actor, owner.User.ID); !errors.Is(err, accounts.ErrLastOwner) {
		t.Fatalf("sole owner removal: got %v, want ErrLastOwner", err)
	}

	memberActor := auth.Membership{WorkspaceID: owner.Workspace.ID, UserID: invitee.User.ID, Role: auth.RoleMember}
	if _, err := service.UpdateMemberRole(ctx, memberActor, owner.User.ID, auth.RoleMember); !errors.Is(err, accounts.ErrOwnerRoleRequired) {
		t.Fatalf("member demoting an owner: got %v", err)
	}
	if err := service.RemoveMember(ctx, memberActor, owner.User.ID); !errors.Is(err, accounts.ErrNotPermitted) {
		t.Fatalf("member removing an owner: got %v", err)
	}

	if _, err := service.UpdateMemberRole(ctx, actor, invitee.User.ID, auth.RoleOwner); err != nil {
		t.Fatalf("promote to owner: %v", err)
	}
	if err := service.RemoveMember(ctx, actor, owner.User.ID); err != nil {
		t.Fatalf("removing an owner with a second owner present: %v", err)
	}

	members, err := service.ListMembers(ctx, owner.Workspace.ID)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 1 || members[0].UserID != invitee.User.ID || members[0].Role != auth.RoleOwner {
		t.Fatalf("members %+v", members)
	}
}

func TestInviteExpiryIsEnforced(t *testing.T) {
	pool := dbtest.New(t)
	past := time.Now().UTC().Add(-8 * 24 * time.Hour)
	inviter, err := accounts.NewService(pool, accounts.WithClock(func() time.Time { return past }))
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	service, err := accounts.NewService(pool)
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	ctx := context.Background()

	owner, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("ensure owner: %v", err)
	}
	invitee, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "member@example.com",
	})
	if err != nil {
		t.Fatalf("ensure member: %v", err)
	}

	created, err := inviter.CreateInvite(ctx, accounts.CreateInviteParams{
		Actor: auth.Membership{WorkspaceID: owner.Workspace.ID, UserID: owner.User.ID, Role: auth.RoleOwner},
		Email: "member@example.com",
	})
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	if _, err := service.AcceptInvite(ctx, created.Token, invitee.User); !errors.Is(err, accounts.ErrInviteExpired) {
		t.Fatalf("got %v, want ErrInviteExpired", err)
	}
}

func TestAnExpiredInviteDoesNotBlockANewOne(t *testing.T) {
	pool := dbtest.New(t)
	past := time.Now().UTC().Add(-8 * 24 * time.Hour)
	inviter, err := accounts.NewService(pool, accounts.WithClock(func() time.Time { return past }))
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	service, err := accounts.NewService(pool)
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	ctx := context.Background()

	owner, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("ensure owner: %v", err)
	}
	invitee, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "member@example.com",
	})
	if err != nil {
		t.Fatalf("ensure member: %v", err)
	}

	actor := auth.Membership{WorkspaceID: owner.Workspace.ID, UserID: owner.User.ID, Role: auth.RoleOwner}
	stale, err := inviter.CreateInvite(ctx, accounts.CreateInviteParams{Actor: actor, Email: "member@example.com"})
	if err != nil {
		t.Fatalf("create the expired invite: %v", err)
	}

	fresh, err := service.CreateInvite(ctx, accounts.CreateInviteParams{Actor: actor, Email: "member@example.com"})
	if err != nil {
		t.Fatalf("create a replacement invite: %v", err)
	}
	if fresh.Token == stale.Token {
		t.Fatal("the replacement invite reused the expired token")
	}

	if _, err := service.AcceptInvite(ctx, stale.Token, invitee.User); !errors.Is(err, accounts.ErrInviteNotFound) {
		t.Fatalf("accepting the superseded invite: got %v, want ErrInviteNotFound", err)
	}

	accepted, err := service.AcceptInvite(ctx, fresh.Token, invitee.User)
	if err != nil {
		t.Fatalf("accept the replacement invite: %v", err)
	}
	if accepted.Workspace.ID != owner.Workspace.ID || accepted.Role != auth.RoleMember {
		t.Fatalf("accepted %+v", accepted)
	}
}

func TestAnOutstandingInviteStillBlocksADuplicate(t *testing.T) {
	service := newService(t)
	ctx := context.Background()

	owner, err := service.EnsureAccountForContact(ctx, auth.VerifiedContact{
		Channel: auth.ChannelEmail, Destination: "owner@example.com",
	})
	if err != nil {
		t.Fatalf("ensure owner: %v", err)
	}

	actor := auth.Membership{WorkspaceID: owner.Workspace.ID, UserID: owner.User.ID, Role: auth.RoleOwner}
	if _, err := service.CreateInvite(ctx, accounts.CreateInviteParams{Actor: actor, Email: "member@example.com"}); err != nil {
		t.Fatalf("create invite: %v", err)
	}
	if _, err := service.CreateInvite(ctx, accounts.CreateInviteParams{
		Actor: actor, Email: "member@example.com",
	}); !errors.Is(err, accounts.ErrInviteExists) {
		t.Fatalf("got %v, want ErrInviteExists", err)
	}
}
