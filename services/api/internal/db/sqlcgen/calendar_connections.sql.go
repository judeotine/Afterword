package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createCalendarConnection = `-- name: CreateCalendarConnection :one
INSERT INTO calendar_connections (user_id, provider, refresh_token_enc, auto_join_rule)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, provider, refresh_token_enc, auto_join_rule, created_at
`

type CreateCalendarConnectionParams struct {
	UserID          uuid.UUID `json:"user_id"`
	Provider        string    `json:"provider"`
	RefreshTokenEnc []byte    `json:"refresh_token_enc"`
	AutoJoinRule    []byte    `json:"auto_join_rule"`
}

func (q *Queries) CreateCalendarConnection(ctx context.Context, arg CreateCalendarConnectionParams) (CalendarConnection, error) {
	row := q.db.QueryRow(ctx, createCalendarConnection,
		arg.UserID,
		arg.Provider,
		arg.RefreshTokenEnc,
		arg.AutoJoinRule,
	)
	var i CalendarConnection
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.Provider,
		&i.RefreshTokenEnc,
		&i.AutoJoinRule,
		&i.CreatedAt,
	)
	return i, err
}

const deleteCalendarConnection = `-- name: DeleteCalendarConnection :execrows
DELETE FROM calendar_connections
WHERE id = $1 AND user_id = $2
`

type DeleteCalendarConnectionParams struct {
	ID     uuid.UUID `json:"id"`
	UserID uuid.UUID `json:"user_id"`
}

func (q *Queries) DeleteCalendarConnection(ctx context.Context, arg DeleteCalendarConnectionParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteCalendarConnection, arg.ID, arg.UserID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getCalendarConnection = `-- name: GetCalendarConnection :one
SELECT calendar_connections.id, calendar_connections.user_id, calendar_connections.provider, calendar_connections.refresh_token_enc, calendar_connections.auto_join_rule, calendar_connections.created_at FROM calendar_connections
JOIN memberships ON memberships.user_id = calendar_connections.user_id
WHERE calendar_connections.id = $1 AND memberships.workspace_id = $2
`

type GetCalendarConnectionParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetCalendarConnection(ctx context.Context, arg GetCalendarConnectionParams) (CalendarConnection, error) {
	row := q.db.QueryRow(ctx, getCalendarConnection, arg.ID, arg.WorkspaceID)
	var i CalendarConnection
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.Provider,
		&i.RefreshTokenEnc,
		&i.AutoJoinRule,
		&i.CreatedAt,
	)
	return i, err
}

const listCalendarConnectionsByWorkspace = `-- name: ListCalendarConnectionsByWorkspace :many
SELECT DISTINCT ON (calendar_connections.created_at, calendar_connections.id) calendar_connections.id, calendar_connections.user_id, calendar_connections.provider, calendar_connections.refresh_token_enc, calendar_connections.auto_join_rule, calendar_connections.created_at
FROM calendar_connections
JOIN memberships ON memberships.user_id = calendar_connections.user_id
WHERE memberships.workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (calendar_connections.created_at, calendar_connections.id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY calendar_connections.created_at DESC, calendar_connections.id DESC
LIMIT $4
`

type ListCalendarConnectionsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListCalendarConnectionsByWorkspace(ctx context.Context, arg ListCalendarConnectionsByWorkspaceParams) ([]CalendarConnection, error) {
	rows, err := q.db.Query(ctx, listCalendarConnectionsByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CalendarConnection{}
	for rows.Next() {
		var i CalendarConnection
		if err := rows.Scan(
			&i.ID,
			&i.UserID,
			&i.Provider,
			&i.RefreshTokenEnc,
			&i.AutoJoinRule,
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

const updateCalendarConnection = `-- name: UpdateCalendarConnection :one
UPDATE calendar_connections SET
    refresh_token_enc = COALESCE($1, refresh_token_enc),
    auto_join_rule = COALESCE($2, auto_join_rule)
WHERE id = $3 AND user_id = $4
RETURNING id, user_id, provider, refresh_token_enc, auto_join_rule, created_at
`

type UpdateCalendarConnectionParams struct {
	RefreshTokenEnc []byte    `json:"refresh_token_enc"`
	AutoJoinRule    []byte    `json:"auto_join_rule"`
	ID              uuid.UUID `json:"id"`
	UserID          uuid.UUID `json:"user_id"`
}

func (q *Queries) UpdateCalendarConnection(ctx context.Context, arg UpdateCalendarConnectionParams) (CalendarConnection, error) {
	row := q.db.QueryRow(ctx, updateCalendarConnection,
		arg.RefreshTokenEnc,
		arg.AutoJoinRule,
		arg.ID,
		arg.UserID,
	)
	var i CalendarConnection
	err := row.Scan(
		&i.ID,
		&i.UserID,
		&i.Provider,
		&i.RefreshTokenEnc,
		&i.AutoJoinRule,
		&i.CreatedAt,
	)
	return i, err
}
