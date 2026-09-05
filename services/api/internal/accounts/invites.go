package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

type CreateInviteParams struct {
	Actor auth.Membership
	Email string
	Role  auth.Role
}

type CreatedInvite struct {
	Invite Invite
	Token  string
}

func (s *Service) CreateInvite(ctx context.Context, params CreateInviteParams) (CreatedInvite, error) {
	email, err := auth.NormalizeEmail(params.Email)
	if err != nil {
		return CreatedInvite{}, ErrInvalidEmail
	}
	role := params.Role
	if role == "" {
		role = auth.RoleMember
	}
	if !role.Valid() {
		return CreatedInvite{}, auth.ErrInvalidRole
	}
	if role == auth.RoleOwner && params.Actor.Role != auth.RoleOwner {
		return CreatedInvite{}, ErrOwnerRoleRequired
	}

	token, err := auth.NewOpaqueToken(InviteTokenBytes)
	if err != nil {
		return CreatedInvite{}, err
	}

	now := s.clock().UTC()
	var invite Invite

	err = s.inTx(ctx, func(q *sqlcgen.Queries) error {
		if _, err := q.RevokeExpiredWorkspaceInvites(ctx, sqlcgen.RevokeExpiredWorkspaceInvitesParams{
			RevokedAt:   timestamp(now),
			WorkspaceID: params.Actor.WorkspaceID,
			Email:       email,
		}); err != nil {
			return fmt.Errorf("revoke expired invites: %w", err)
		}

		existing, err := q.GetUserByEmail(ctx, email)
		switch {
		case err == nil:
			if _, err := q.GetMembership(ctx, sqlcgen.GetMembershipParams{
				WorkspaceID: params.Actor.WorkspaceID,
				UserID:      existing.ID,
			}); err == nil {
				return ErrAlreadyMember
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read membership: %w", err)
			}
		case errors.Is(err, pgx.ErrNoRows):
		default:
			return fmt.Errorf("read user by email: %w", err)
		}

		row, err := q.CreateWorkspaceInvite(ctx, sqlcgen.CreateWorkspaceInviteParams{
			WorkspaceID:     params.Actor.WorkspaceID,
			Email:           email,
			Role:            string(role),
			TokenHash:       auth.HashToken(token),
			InvitedByUserID: &params.Actor.UserID,
			ExpiresAt:       timestamp(now.Add(s.inviteTTL)),
		})
		if err != nil {
			return fmt.Errorf("create invite: %w", err)
		}
		invite = inviteFromRow(row)
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return CreatedInvite{}, ErrInviteExists
		}
		return CreatedInvite{}, err
	}

	return CreatedInvite{Invite: invite, Token: token}, nil
}

func (s *Service) ListInvites(ctx context.Context, workspaceID uuid.UUID, pageSize int32) ([]Invite, error) {
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 100
	}
	rows, err := s.queries.ListWorkspaceInvites(ctx, sqlcgen.ListWorkspaceInvitesParams{
		WorkspaceID: workspaceID,
		PageSize:    pageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("list invites: %w", err)
	}
	invites := make([]Invite, 0, len(rows))
	for _, row := range rows {
		invites = append(invites, inviteFromRow(row))
	}
	return invites, nil
}

func (s *Service) RevokeInvite(ctx context.Context, workspaceID, inviteID uuid.UUID) error {
	rows, err := s.queries.RevokeWorkspaceInvite(ctx, sqlcgen.RevokeWorkspaceInviteParams{
		RevokedAt:   timestamp(s.clock().UTC()),
		ID:          inviteID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return fmt.Errorf("revoke invite: %w", err)
	}
	if rows == 0 {
		return ErrInviteNotFound
	}
	return nil
}

type AcceptedInvite struct {
	Workspace Workspace
	Role      auth.Role
}

func (s *Service) AcceptInvite(ctx context.Context, token string, user User) (AcceptedInvite, error) {
	if token == "" {
		return AcceptedInvite{}, ErrInviteNotFound
	}

	now := s.clock().UTC()
	var accepted AcceptedInvite

	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		row, err := q.GetWorkspaceInviteByTokenHash(ctx, auth.HashToken(token))
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInviteNotFound
			}
			return fmt.Errorf("read invite: %w", err)
		}

		invite := inviteFromRow(row)
		if row.RevokedAt.Valid {
			return ErrInviteNotFound
		}
		if invite.AcceptedAt != nil {
			return ErrInviteAlreadyUsed
		}
		if !now.Before(invite.ExpiresAt) {
			return ErrInviteExpired
		}
		if user.Email == "" || !equalEmails(user.Email, invite.Email) {
			return ErrInviteEmailMismatch
		}

		if _, err := q.GetMembership(ctx, sqlcgen.GetMembershipParams{
			WorkspaceID: invite.WorkspaceID,
			UserID:      user.ID,
		}); err == nil {
			return ErrAlreadyMember
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("read membership: %w", err)
		}

		if _, err := q.AcceptWorkspaceInvite(ctx, sqlcgen.AcceptWorkspaceInviteParams{
			AcceptedAt:       timestamp(now),
			AcceptedByUserID: &user.ID,
			ID:               invite.ID,
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInviteAlreadyUsed
			}
			return fmt.Errorf("accept invite: %w", err)
		}

		if _, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
			WorkspaceID: invite.WorkspaceID,
			UserID:      user.ID,
			Role:        string(invite.Role),
		}); err != nil {
			return fmt.Errorf("create membership: %w", err)
		}

		workspaceRow, err := q.GetWorkspace(ctx, invite.WorkspaceID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrWorkspaceNotFound
			}
			return fmt.Errorf("read workspace: %w", err)
		}

		accepted = AcceptedInvite{Workspace: workspaceFromRow(workspaceRow), Role: invite.Role}
		return nil
	})
	if err != nil {
		return AcceptedInvite{}, err
	}
	return accepted, nil
}

func (s *Service) InviteExpiry(now time.Time) time.Time {
	return now.Add(s.inviteTTL)
}

func equalEmails(left, right string) bool {
	return normalizeForCompare(left) == normalizeForCompare(right)
}

func normalizeForCompare(value string) string {
	normalized, err := auth.NormalizeEmail(value)
	if err != nil {
		return ""
	}
	return normalized
}
