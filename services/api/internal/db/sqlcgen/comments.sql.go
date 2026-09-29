package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createComment = `-- name: CreateComment :one
INSERT INTO comments (meeting_id, user_id, at_s, body)
SELECT $1, $2, $3, $4
FROM meetings
WHERE meetings.id = $1 AND meetings.workspace_id = $5
RETURNING id, meeting_id, user_id, at_s, body, created_at
`

type CreateCommentParams struct {
	MeetingID   uuid.UUID  `json:"meeting_id"`
	UserID      *uuid.UUID `json:"user_id"`
	AtS         float64    `json:"at_s"`
	Body        string     `json:"body"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
}

func (q *Queries) CreateComment(ctx context.Context, arg CreateCommentParams) (Comment, error) {
	row := q.db.QueryRow(ctx, createComment,
		arg.MeetingID,
		arg.UserID,
		arg.AtS,
		arg.Body,
		arg.WorkspaceID,
	)
	var i Comment
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.UserID,
		&i.AtS,
		&i.Body,
		&i.CreatedAt,
	)
	return i, err
}

const deleteComment = `-- name: DeleteComment :execrows
DELETE FROM comments
USING meetings
WHERE comments.meeting_id = meetings.id
  AND comments.id = $1
  AND meetings.workspace_id = $2
`

type DeleteCommentParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteComment(ctx context.Context, arg DeleteCommentParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteComment, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getComment = `-- name: GetComment :one
SELECT comments.id, comments.meeting_id, comments.user_id, comments.at_s, comments.body, comments.created_at FROM comments
JOIN meetings ON meetings.id = comments.meeting_id
WHERE comments.id = $1 AND meetings.workspace_id = $2
`

type GetCommentParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetComment(ctx context.Context, arg GetCommentParams) (Comment, error) {
	row := q.db.QueryRow(ctx, getComment, arg.ID, arg.WorkspaceID)
	var i Comment
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.UserID,
		&i.AtS,
		&i.Body,
		&i.CreatedAt,
	)
	return i, err
}

const listCommentsByWorkspace = `-- name: ListCommentsByWorkspace :many
SELECT comments.id, comments.meeting_id, comments.user_id, comments.at_s, comments.body, comments.created_at FROM comments
JOIN meetings ON meetings.id = comments.meeting_id
WHERE meetings.workspace_id = $1
  AND ($2::uuid IS NULL OR comments.meeting_id = $2::uuid)
  AND (
      $3::timestamptz IS NULL
      OR (comments.created_at, comments.id) < ($3::timestamptz, $4::uuid)
  )
ORDER BY comments.created_at DESC, comments.id DESC
LIMIT $5
`

type ListCommentsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	MeetingID       *uuid.UUID         `json:"meeting_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListCommentsByWorkspace(ctx context.Context, arg ListCommentsByWorkspaceParams) ([]Comment, error) {
	rows, err := q.db.Query(ctx, listCommentsByWorkspace,
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
	items := []Comment{}
	for rows.Next() {
		var i Comment
		if err := rows.Scan(
			&i.ID,
			&i.MeetingID,
			&i.UserID,
			&i.AtS,
			&i.Body,
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

const updateComment = `-- name: UpdateComment :one
UPDATE comments SET
    at_s = COALESCE($1, at_s),
    body = COALESCE($2, body)
FROM meetings
WHERE comments.meeting_id = meetings.id
  AND comments.id = $3
  AND meetings.workspace_id = $4
RETURNING comments.id, comments.meeting_id, comments.user_id, comments.at_s, comments.body, comments.created_at
`

type UpdateCommentParams struct {
	AtS         *float64  `json:"at_s"`
	Body        *string   `json:"body"`
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) UpdateComment(ctx context.Context, arg UpdateCommentParams) (Comment, error) {
	row := q.db.QueryRow(ctx, updateComment,
		arg.AtS,
		arg.Body,
		arg.ID,
		arg.WorkspaceID,
	)
	var i Comment
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.UserID,
		&i.AtS,
		&i.Body,
		&i.CreatedAt,
	)
	return i, err
}
