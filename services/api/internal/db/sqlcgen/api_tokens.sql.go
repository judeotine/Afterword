package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createAPIToken = `-- name: CreateAPIToken :one
INSERT INTO api_tokens (workspace_id, hash, scopes)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, hash, scopes, last_used_at, created_at
`

type CreateAPITokenParams struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Hash        string    `json:"hash"`
	Scopes      []string  `json:"scopes"`
}

func (q *Queries) CreateAPIToken(ctx context.Context, arg CreateAPITokenParams) (ApiToken, error) {
	row := q.db.QueryRow(ctx, createAPIToken, arg.WorkspaceID, arg.Hash, arg.Scopes)
	var i ApiToken
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Hash,
		&i.Scopes,
		&i.LastUsedAt,
		&i.CreatedAt,
	)
	return i, err
}

const deleteAPIToken = `-- name: DeleteAPIToken :execrows
DELETE FROM api_tokens
WHERE id = $1 AND workspace_id = $2
`

type DeleteAPITokenParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteAPIToken(ctx context.Context, arg DeleteAPITokenParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteAPIToken, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getAPIToken = `-- name: GetAPIToken :one
SELECT id, workspace_id, hash, scopes, last_used_at, created_at FROM api_tokens
WHERE id = $1 AND workspace_id = $2
`

type GetAPITokenParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetAPIToken(ctx context.Context, arg GetAPITokenParams) (ApiToken, error) {
	row := q.db.QueryRow(ctx, getAPIToken, arg.ID, arg.WorkspaceID)
	var i ApiToken
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Hash,
		&i.Scopes,
		&i.LastUsedAt,
		&i.CreatedAt,
	)
	return i, err
}

const getAPITokenByHash = `-- name: GetAPITokenByHash :one
SELECT id, workspace_id, hash, scopes, last_used_at, created_at FROM api_tokens WHERE hash = $1
`

func (q *Queries) GetAPITokenByHash(ctx context.Context, hash string) (ApiToken, error) {
	row := q.db.QueryRow(ctx, getAPITokenByHash, hash)
	var i ApiToken
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Hash,
		&i.Scopes,
		&i.LastUsedAt,
		&i.CreatedAt,
	)
	return i, err
}

const listAPITokensByWorkspace = `-- name: ListAPITokensByWorkspace :many
SELECT id, workspace_id, hash, scopes, last_used_at, created_at FROM api_tokens
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListAPITokensByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListAPITokensByWorkspace(ctx context.Context, arg ListAPITokensByWorkspaceParams) ([]ApiToken, error) {
	rows, err := q.db.Query(ctx, listAPITokensByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []ApiToken{}
	for rows.Next() {
		var i ApiToken
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Hash,
			&i.Scopes,
			&i.LastUsedAt,
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

const updateAPIToken = `-- name: UpdateAPIToken :one
UPDATE api_tokens SET
    scopes = COALESCE($1, scopes),
    last_used_at = COALESCE($2, last_used_at)
WHERE id = $3 AND workspace_id = $4
RETURNING id, workspace_id, hash, scopes, last_used_at, created_at
`

type UpdateAPITokenParams struct {
	Scopes      []string           `json:"scopes"`
	LastUsedAt  pgtype.Timestamptz `json:"last_used_at"`
	ID          uuid.UUID          `json:"id"`
	WorkspaceID uuid.UUID          `json:"workspace_id"`
}

func (q *Queries) UpdateAPIToken(ctx context.Context, arg UpdateAPITokenParams) (ApiToken, error) {
	row := q.db.QueryRow(ctx, updateAPIToken,
		arg.Scopes,
		arg.LastUsedAt,
		arg.ID,
		arg.WorkspaceID,
	)
	var i ApiToken
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Hash,
		&i.Scopes,
		&i.LastUsedAt,
		&i.CreatedAt,
	)
	return i, err
}
