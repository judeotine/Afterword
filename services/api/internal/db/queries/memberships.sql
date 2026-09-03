-- name: CreateMembership :one
INSERT INTO memberships (workspace_id, user_id, role)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role))
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: ListMembershipsByWorkspace :many
SELECT * FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, user_id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, user_id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateMembership :one
UPDATE memberships SET role = sqlc.arg(role)
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteMembership :execrows
DELETE FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);
