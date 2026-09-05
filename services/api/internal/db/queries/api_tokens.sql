-- name: CreateAPIToken :one
INSERT INTO api_tokens (workspace_id, hash, scopes)
VALUES (sqlc.arg(workspace_id), sqlc.arg(hash), sqlc.arg(scopes))
RETURNING *;

-- name: GetAPIToken :one
SELECT * FROM api_tokens
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: GetAPITokenByHash :one
SELECT * FROM api_tokens WHERE hash = sqlc.arg(hash);

-- name: ListAPITokensByWorkspace :many
SELECT * FROM api_tokens
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateAPIToken :one
UPDATE api_tokens SET
    scopes = COALESCE(sqlc.narg(scopes), scopes),
    last_used_at = COALESCE(sqlc.narg(last_used_at), last_used_at)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteAPIToken :execrows
DELETE FROM api_tokens
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
