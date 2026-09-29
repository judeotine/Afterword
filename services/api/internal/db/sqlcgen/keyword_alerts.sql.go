package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createKeywordAlert = `-- name: CreateKeywordAlert :one
INSERT INTO keyword_alerts (workspace_id, user_id, phrase, channel)
VALUES ($1, $2, $3, $4)
RETURNING id, workspace_id, user_id, phrase, channel, created_at
`

type CreateKeywordAlertParams struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	UserID      uuid.UUID `json:"user_id"`
	Phrase      string    `json:"phrase"`
	Channel     string    `json:"channel"`
}

func (q *Queries) CreateKeywordAlert(ctx context.Context, arg CreateKeywordAlertParams) (KeywordAlert, error) {
	row := q.db.QueryRow(ctx, createKeywordAlert,
		arg.WorkspaceID,
		arg.UserID,
		arg.Phrase,
		arg.Channel,
	)
	var i KeywordAlert
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.UserID,
		&i.Phrase,
		&i.Channel,
		&i.CreatedAt,
	)
	return i, err
}

const deleteKeywordAlert = `-- name: DeleteKeywordAlert :execrows
DELETE FROM keyword_alerts
WHERE id = $1 AND workspace_id = $2
`

type DeleteKeywordAlertParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteKeywordAlert(ctx context.Context, arg DeleteKeywordAlertParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteKeywordAlert, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getKeywordAlert = `-- name: GetKeywordAlert :one
SELECT id, workspace_id, user_id, phrase, channel, created_at FROM keyword_alerts
WHERE id = $1 AND workspace_id = $2
`

type GetKeywordAlertParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetKeywordAlert(ctx context.Context, arg GetKeywordAlertParams) (KeywordAlert, error) {
	row := q.db.QueryRow(ctx, getKeywordAlert, arg.ID, arg.WorkspaceID)
	var i KeywordAlert
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.UserID,
		&i.Phrase,
		&i.Channel,
		&i.CreatedAt,
	)
	return i, err
}

const listKeywordAlertsByWorkspace = `-- name: ListKeywordAlertsByWorkspace :many
SELECT id, workspace_id, user_id, phrase, channel, created_at FROM keyword_alerts
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListKeywordAlertsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListKeywordAlertsByWorkspace(ctx context.Context, arg ListKeywordAlertsByWorkspaceParams) ([]KeywordAlert, error) {
	rows, err := q.db.Query(ctx, listKeywordAlertsByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []KeywordAlert{}
	for rows.Next() {
		var i KeywordAlert
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.UserID,
			&i.Phrase,
			&i.Channel,
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

const updateKeywordAlert = `-- name: UpdateKeywordAlert :one
UPDATE keyword_alerts SET
    phrase = COALESCE($1, phrase),
    channel = COALESCE($2, channel)
WHERE id = $3 AND workspace_id = $4
RETURNING id, workspace_id, user_id, phrase, channel, created_at
`

type UpdateKeywordAlertParams struct {
	Phrase      *string   `json:"phrase"`
	Channel     *string   `json:"channel"`
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) UpdateKeywordAlert(ctx context.Context, arg UpdateKeywordAlertParams) (KeywordAlert, error) {
	row := q.db.QueryRow(ctx, updateKeywordAlert,
		arg.Phrase,
		arg.Channel,
		arg.ID,
		arg.WorkspaceID,
	)
	var i KeywordAlert
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.UserID,
		&i.Phrase,
		&i.Channel,
		&i.CreatedAt,
	)
	return i, err
}
