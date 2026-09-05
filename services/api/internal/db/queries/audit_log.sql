-- name: CreateAuditLogEntry :one
INSERT INTO audit_log (workspace_id, actor_user_id, actor, action, target, at)
VALUES (sqlc.arg(workspace_id), sqlc.narg(actor_user_id), sqlc.arg(actor), sqlc.arg(action), sqlc.arg(target), sqlc.arg(at))
RETURNING *;

-- name: GetAuditLogEntry :one
SELECT * FROM audit_log
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListAuditLogByWorkspace :many
SELECT * FROM audit_log
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateAuditLogEntry :one
UPDATE audit_log SET target = COALESCE(sqlc.narg(target), target)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteAuditLogEntry :execrows
DELETE FROM audit_log
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
