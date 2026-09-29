package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createFolder = `-- name: CreateFolder :one
INSERT INTO folders (workspace_id, name, parent_id)
VALUES ($1, $2, $3)
RETURNING id, workspace_id, name, parent_id, created_at
`

type CreateFolderParams struct {
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Name        string     `json:"name"`
	ParentID    *uuid.UUID `json:"parent_id"`
}

func (q *Queries) CreateFolder(ctx context.Context, arg CreateFolderParams) (Folder, error) {
	row := q.db.QueryRow(ctx, createFolder, arg.WorkspaceID, arg.Name, arg.ParentID)
	var i Folder
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Name,
		&i.ParentID,
		&i.CreatedAt,
	)
	return i, err
}

const deleteFolder = `-- name: DeleteFolder :execrows
DELETE FROM folders
WHERE id = $1 AND workspace_id = $2
`

type DeleteFolderParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteFolder(ctx context.Context, arg DeleteFolderParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteFolder, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getFolder = `-- name: GetFolder :one
SELECT id, workspace_id, name, parent_id, created_at FROM folders
WHERE id = $1 AND workspace_id = $2
`

type GetFolderParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetFolder(ctx context.Context, arg GetFolderParams) (Folder, error) {
	row := q.db.QueryRow(ctx, getFolder, arg.ID, arg.WorkspaceID)
	var i Folder
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Name,
		&i.ParentID,
		&i.CreatedAt,
	)
	return i, err
}

const listFoldersByWorkspace = `-- name: ListFoldersByWorkspace :many
SELECT id, workspace_id, name, parent_id, created_at FROM folders
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListFoldersByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListFoldersByWorkspace(ctx context.Context, arg ListFoldersByWorkspaceParams) ([]Folder, error) {
	rows, err := q.db.Query(ctx, listFoldersByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Folder{}
	for rows.Next() {
		var i Folder
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Name,
			&i.ParentID,
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

const updateFolder = `-- name: UpdateFolder :one
UPDATE folders SET
    name = COALESCE($1, name),
    parent_id = COALESCE($2, parent_id)
WHERE id = $3 AND workspace_id = $4
RETURNING id, workspace_id, name, parent_id, created_at
`

type UpdateFolderParams struct {
	Name        *string    `json:"name"`
	ParentID    *uuid.UUID `json:"parent_id"`
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
}

func (q *Queries) UpdateFolder(ctx context.Context, arg UpdateFolderParams) (Folder, error) {
	row := q.db.QueryRow(ctx, updateFolder,
		arg.Name,
		arg.ParentID,
		arg.ID,
		arg.WorkspaceID,
	)
	var i Folder
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Name,
		&i.ParentID,
		&i.CreatedAt,
	)
	return i, err
}
