package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createShareLink = `-- name: CreateShareLink :one
INSERT INTO share_links (meeting_id, token_hash, permission, expires_at)
SELECT $1, $2, $3, $4
FROM meetings
WHERE meetings.id = $1 AND meetings.workspace_id = $5
RETURNING id, meeting_id, permission, expires_at, created_at, token_hash
`

type CreateShareLinkParams struct {
	MeetingID   uuid.UUID          `json:"meeting_id"`
	TokenHash   string             `json:"token_hash"`
	Permission  string             `json:"permission"`
	ExpiresAt   pgtype.Timestamptz `json:"expires_at"`
	WorkspaceID uuid.UUID          `json:"workspace_id"`
}

func (q *Queries) CreateShareLink(ctx context.Context, arg CreateShareLinkParams) (ShareLink, error) {
	row := q.db.QueryRow(ctx, createShareLink,
		arg.MeetingID,
		arg.TokenHash,
		arg.Permission,
		arg.ExpiresAt,
		arg.WorkspaceID,
	)
	var i ShareLink
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.Permission,
		&i.ExpiresAt,
		&i.CreatedAt,
		&i.TokenHash,
	)
	return i, err
}

const deleteShareLink = `-- name: DeleteShareLink :execrows
DELETE FROM share_links
USING meetings
WHERE share_links.meeting_id = meetings.id
  AND share_links.id = $1
  AND meetings.workspace_id = $2
`

type DeleteShareLinkParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteShareLink(ctx context.Context, arg DeleteShareLinkParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteShareLink, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getShareLink = `-- name: GetShareLink :one
SELECT share_links.id, share_links.meeting_id, share_links.permission, share_links.expires_at, share_links.created_at, share_links.token_hash FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE share_links.id = $1 AND meetings.workspace_id = $2
`

type GetShareLinkParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetShareLink(ctx context.Context, arg GetShareLinkParams) (ShareLink, error) {
	row := q.db.QueryRow(ctx, getShareLink, arg.ID, arg.WorkspaceID)
	var i ShareLink
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.Permission,
		&i.ExpiresAt,
		&i.CreatedAt,
		&i.TokenHash,
	)
	return i, err
}

const getShareLinkByTokenHash = `-- name: GetShareLinkByTokenHash :one
SELECT id, meeting_id, permission, expires_at, created_at, token_hash FROM share_links WHERE token_hash = $1
`

func (q *Queries) GetShareLinkByTokenHash(ctx context.Context, tokenHash string) (ShareLink, error) {
	row := q.db.QueryRow(ctx, getShareLinkByTokenHash, tokenHash)
	var i ShareLink
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.Permission,
		&i.ExpiresAt,
		&i.CreatedAt,
		&i.TokenHash,
	)
	return i, err
}

const listShareLinksByWorkspace = `-- name: ListShareLinksByWorkspace :many
SELECT share_links.id, share_links.meeting_id, share_links.permission, share_links.expires_at, share_links.created_at, share_links.token_hash FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE meetings.workspace_id = $1
  AND ($2::uuid IS NULL OR share_links.meeting_id = $2::uuid)
  AND (
      $3::timestamptz IS NULL
      OR (share_links.created_at, share_links.id) < ($3::timestamptz, $4::uuid)
  )
ORDER BY share_links.created_at DESC, share_links.id DESC
LIMIT $5
`

type ListShareLinksByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	MeetingID       *uuid.UUID         `json:"meeting_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListShareLinksByWorkspace(ctx context.Context, arg ListShareLinksByWorkspaceParams) ([]ShareLink, error) {
	rows, err := q.db.Query(ctx, listShareLinksByWorkspace,
		arg.WorkspaceID,
		arg.MeetingID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ShareLink{}
	for rows.Next() {
		var i ShareLink
		if err := rows.Scan(
			&i.ID,
			&i.MeetingID,
			&i.Permission,
			&i.ExpiresAt,
			&i.CreatedAt,
			&i.TokenHash,
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

const updateShareLink = `-- name: UpdateShareLink :one
UPDATE share_links SET
    permission = COALESCE($1, permission),
    expires_at = COALESCE($2, expires_at)
FROM meetings
WHERE share_links.meeting_id = meetings.id
  AND share_links.id = $3
  AND meetings.workspace_id = $4
RETURNING share_links.id, share_links.meeting_id, share_links.permission, share_links.expires_at, share_links.created_at, share_links.token_hash
`

type UpdateShareLinkParams struct {
	Permission  *string            `json:"permission"`
	ExpiresAt   pgtype.Timestamptz `json:"expires_at"`
	ID          uuid.UUID          `json:"id"`
	WorkspaceID uuid.UUID          `json:"workspace_id"`
}

func (q *Queries) UpdateShareLink(ctx context.Context, arg UpdateShareLinkParams) (ShareLink, error) {
	row := q.db.QueryRow(ctx, updateShareLink,
		arg.Permission,
		arg.ExpiresAt,
		arg.ID,
		arg.WorkspaceID,
	)
	var i ShareLink
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.Permission,
		&i.ExpiresAt,
		&i.CreatedAt,
		&i.TokenHash,
	)
	return i, err
}
