package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createClip = `-- name: CreateClip :one
INSERT INTO clips (meeting_id, start_s, end_s, title, object, share_token)
SELECT $1, $2, $3, $4,
       $5, $6
FROM meetings
WHERE meetings.id = $1 AND meetings.workspace_id = $7
RETURNING id, meeting_id, start_s, end_s, title, object, share_token, created_at
`

type CreateClipParams struct {
	MeetingID   uuid.UUID `json:"meeting_id"`
	StartS      float64   `json:"start_s"`
	EndS        float64   `json:"end_s"`
	Title       string    `json:"title"`
	Object      *string   `json:"object"`
	ShareToken  *string   `json:"share_token"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) CreateClip(ctx context.Context, arg CreateClipParams) (Clip, error) {
	row := q.db.QueryRow(ctx, createClip,
		arg.MeetingID,
		arg.StartS,
		arg.EndS,
		arg.Title,
		arg.Object,
		arg.ShareToken,
		arg.WorkspaceID,
	)
	var i Clip
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.StartS,
		&i.EndS,
		&i.Title,
		&i.Object,
		&i.ShareToken,
		&i.CreatedAt,
	)
	return i, err
}

const deleteClip = `-- name: DeleteClip :execrows
DELETE FROM clips
USING meetings
WHERE clips.meeting_id = meetings.id
  AND clips.id = $1
  AND meetings.workspace_id = $2
`

type DeleteClipParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteClip(ctx context.Context, arg DeleteClipParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteClip, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getClip = `-- name: GetClip :one
SELECT clips.id, clips.meeting_id, clips.start_s, clips.end_s, clips.title, clips.object, clips.share_token, clips.created_at FROM clips
JOIN meetings ON meetings.id = clips.meeting_id
WHERE clips.id = $1 AND meetings.workspace_id = $2
`

type GetClipParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetClip(ctx context.Context, arg GetClipParams) (Clip, error) {
	row := q.db.QueryRow(ctx, getClip, arg.ID, arg.WorkspaceID)
	var i Clip
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.StartS,
		&i.EndS,
		&i.Title,
		&i.Object,
		&i.ShareToken,
		&i.CreatedAt,
	)
	return i, err
}

const listClipsByWorkspace = `-- name: ListClipsByWorkspace :many
SELECT clips.id, clips.meeting_id, clips.start_s, clips.end_s, clips.title, clips.object, clips.share_token, clips.created_at FROM clips
JOIN meetings ON meetings.id = clips.meeting_id
WHERE meetings.workspace_id = $1
  AND ($2::uuid IS NULL OR clips.meeting_id = $2::uuid)
  AND (
      $3::timestamptz IS NULL
      OR (clips.created_at, clips.id) < ($3::timestamptz, $4::uuid)
  )
ORDER BY clips.created_at DESC, clips.id DESC
LIMIT $5
`

type ListClipsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	MeetingID       *uuid.UUID         `json:"meeting_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListClipsByWorkspace(ctx context.Context, arg ListClipsByWorkspaceParams) ([]Clip, error) {
	rows, err := q.db.Query(ctx, listClipsByWorkspace,
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
	items := []Clip{}
	for rows.Next() {
		var i Clip
		if err := rows.Scan(
			&i.ID,
			&i.MeetingID,
			&i.StartS,
			&i.EndS,
			&i.Title,
			&i.Object,
			&i.ShareToken,
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

const updateClip = `-- name: UpdateClip :one
UPDATE clips SET
    start_s = COALESCE($1, clips.start_s),
    end_s = COALESCE($2, clips.end_s),
    title = COALESCE($3, clips.title),
    object = COALESCE($4, clips.object),
    share_token = COALESCE($5, clips.share_token)
FROM meetings
WHERE clips.meeting_id = meetings.id
  AND clips.id = $6
  AND meetings.workspace_id = $7
RETURNING clips.id, clips.meeting_id, clips.start_s, clips.end_s, clips.title, clips.object, clips.share_token, clips.created_at
`

type UpdateClipParams struct {
	StartS      *float64  `json:"start_s"`
	EndS        *float64  `json:"end_s"`
	Title       *string   `json:"title"`
	Object      *string   `json:"object"`
	ShareToken  *string   `json:"share_token"`
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) UpdateClip(ctx context.Context, arg UpdateClipParams) (Clip, error) {
	row := q.db.QueryRow(ctx, updateClip,
		arg.StartS,
		arg.EndS,
		arg.Title,
		arg.Object,
		arg.ShareToken,
		arg.ID,
		arg.WorkspaceID,
	)
	var i Clip
	err := row.Scan(
		&i.ID,
		&i.MeetingID,
		&i.StartS,
		&i.EndS,
		&i.Title,
		&i.Object,
		&i.ShareToken,
		&i.CreatedAt,
	)
	return i, err
}
