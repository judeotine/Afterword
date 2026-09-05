-- name: CreateFolder :one
INSERT INTO folders (workspace_id, name, parent_id)
VALUES (sqlc.arg(workspace_id), sqlc.arg(name), sqlc.narg(parent_id))
RETURNING *;

-- name: GetFolder :one
SELECT * FROM folders
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListFoldersByWorkspace :many
SELECT * FROM folders
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateFolder :one
UPDATE folders SET
    name = COALESCE(sqlc.narg(name), name),
    parent_id = COALESCE(sqlc.narg(parent_id), parent_id)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteFolder :execrows
DELETE FROM folders
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
