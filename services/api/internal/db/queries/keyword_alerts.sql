-- name: CreateKeywordAlert :one
INSERT INTO keyword_alerts (workspace_id, user_id, phrase, channel)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(phrase), sqlc.arg(channel))
RETURNING *;

-- name: GetKeywordAlert :one
SELECT * FROM keyword_alerts
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListKeywordAlertsByWorkspace :many
SELECT * FROM keyword_alerts
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateKeywordAlert :one
UPDATE keyword_alerts SET
    phrase = COALESCE(sqlc.narg(phrase), phrase),
    channel = COALESCE(sqlc.narg(channel), channel)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteKeywordAlert :execrows
DELETE FROM keyword_alerts
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
