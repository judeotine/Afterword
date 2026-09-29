package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const acceptWorkspaceInvite = `-- name: AcceptWorkspaceInvite :one
UPDATE workspace_invites
SET accepted_at = $1, accepted_by_user_id = $2
WHERE id = $3 AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING id, workspace_id, email, role, token_hash, invited_by_user_id, expires_at, accepted_at, accepted_by_user_id, revoked_at, created_at
`

type AcceptWorkspaceInviteParams struct {
	AcceptedAt       pgtype.Timestamptz `json:"accepted_at"`
	AcceptedByUserID *uuid.UUID         `json:"accepted_by_user_id"`
	ID               uuid.UUID          `json:"id"`
}

func (q *Queries) AcceptWorkspaceInvite(ctx context.Context, arg AcceptWorkspaceInviteParams) (WorkspaceInvite, error) {
	row := q.db.QueryRow(ctx, acceptWorkspaceInvite, arg.AcceptedAt, arg.AcceptedByUserID, arg.ID)
	var i WorkspaceInvite
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Email,
		&i.Role,
		&i.TokenHash,
		&i.InvitedByUserID,
		&i.ExpiresAt,
		&i.AcceptedAt,
		&i.AcceptedByUserID,
		&i.RevokedAt,
		&i.CreatedAt,
	)
	return i, err
}

const createWorkspaceInvite = `-- name: CreateWorkspaceInvite :one
INSERT INTO workspace_invites (workspace_id, email, role, token_hash, invited_by_user_id, expires_at)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6
)
RETURNING id, workspace_id, email, role, token_hash, invited_by_user_id, expires_at, accepted_at, accepted_by_user_id, revoked_at, created_at
`

type CreateWorkspaceInviteParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	Email           string             `json:"email"`
	Role            string             `json:"role"`
	TokenHash       string             `json:"token_hash"`
	InvitedByUserID *uuid.UUID         `json:"invited_by_user_id"`
	ExpiresAt       pgtype.Timestamptz `json:"expires_at"`
}

func (q *Queries) CreateWorkspaceInvite(ctx context.Context, arg CreateWorkspaceInviteParams) (WorkspaceInvite, error) {
	row := q.db.QueryRow(ctx, createWorkspaceInvite,
		arg.WorkspaceID,
		arg.Email,
		arg.Role,
		arg.TokenHash,
		arg.InvitedByUserID,
		arg.ExpiresAt,
	)
	var i WorkspaceInvite
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Email,
		&i.Role,
		&i.TokenHash,
		&i.InvitedByUserID,
		&i.ExpiresAt,
		&i.AcceptedAt,
		&i.AcceptedByUserID,
		&i.RevokedAt,
		&i.CreatedAt,
	)
	return i, err
}

const getWorkspaceInviteByTokenHash = `-- name: GetWorkspaceInviteByTokenHash :one
SELECT id, workspace_id, email, role, token_hash, invited_by_user_id, expires_at, accepted_at, accepted_by_user_id, revoked_at, created_at FROM workspace_invites WHERE token_hash = $1
`

func (q *Queries) GetWorkspaceInviteByTokenHash(ctx context.Context, tokenHash string) (WorkspaceInvite, error) {
	row := q.db.QueryRow(ctx, getWorkspaceInviteByTokenHash, tokenHash)
	var i WorkspaceInvite
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Email,
		&i.Role,
		&i.TokenHash,
		&i.InvitedByUserID,
		&i.ExpiresAt,
		&i.AcceptedAt,
		&i.AcceptedByUserID,
		&i.RevokedAt,
		&i.CreatedAt,
	)
	return i, err
}

const listWorkspaceInvites = `-- name: ListWorkspaceInvites :many
SELECT id, workspace_id, email, role, token_hash, invited_by_user_id, expires_at, accepted_at, accepted_by_user_id, revoked_at, created_at FROM workspace_invites
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListWorkspaceInvitesParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListWorkspaceInvites(ctx context.Context, arg ListWorkspaceInvitesParams) ([]WorkspaceInvite, error) {
	rows, err := q.db.Query(ctx, listWorkspaceInvites,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []WorkspaceInvite{}
	for rows.Next() {
		var i WorkspaceInvite
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Email,
			&i.Role,
			&i.TokenHash,
			&i.InvitedByUserID,
			&i.ExpiresAt,
			&i.AcceptedAt,
			&i.AcceptedByUserID,
			&i.RevokedAt,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const revokeExpiredWorkspaceInvites = `-- name: RevokeExpiredWorkspaceInvites :execrows
UPDATE workspace_invites SET revoked_at = $1
WHERE workspace_id = $2
  AND email = $3
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND expires_at <= $1
`

type RevokeExpiredWorkspaceInvitesParams struct {
	RevokedAt   pgtype.Timestamptz `json:"revoked_at"`
	WorkspaceID uuid.UUID          `json:"workspace_id"`
	Email       string             `json:"email"`
}

func (q *Queries) RevokeExpiredWorkspaceInvites(ctx context.Context, arg RevokeExpiredWorkspaceInvitesParams) (int64, error) {
	result, err := q.db.Exec(ctx, revokeExpiredWorkspaceInvites, arg.RevokedAt, arg.WorkspaceID, arg.Email)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const revokeWorkspaceInvite = `-- name: RevokeWorkspaceInvite :execrows
UPDATE workspace_invites SET revoked_at = $1
WHERE id = $2
  AND workspace_id = $3
  AND accepted_at IS NULL
  AND revoked_at IS NULL
`

type RevokeWorkspaceInviteParams struct {
	RevokedAt   pgtype.Timestamptz `json:"revoked_at"`
	ID          uuid.UUID          `json:"id"`
	WorkspaceID uuid.UUID          `json:"workspace_id"`
}

func (q *Queries) RevokeWorkspaceInvite(ctx context.Context, arg RevokeWorkspaceInviteParams) (int64, error) {
	result, err := q.db.Exec(ctx, revokeWorkspaceInvite, arg.RevokedAt, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
