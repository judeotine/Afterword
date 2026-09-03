-- name: CreateBotJob :one
INSERT INTO bot_jobs (workspace_id, meeting_url, platform, scheduled_at, status, worker_id)
VALUES (sqlc.arg(workspace_id), sqlc.arg(meeting_url), sqlc.arg(platform), sqlc.arg(scheduled_at),
        sqlc.arg(status), sqlc.narg(worker_id))
RETURNING *;

-- name: GetBotJob :one
SELECT * FROM bot_jobs
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListBotJobsByWorkspace :many
SELECT * FROM bot_jobs
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateBotJob :one
UPDATE bot_jobs SET
    status = COALESCE(sqlc.narg(status), status),
    worker_id = COALESCE(sqlc.narg(worker_id), worker_id),
    minutes_used = COALESCE(sqlc.narg(minutes_used), minutes_used),
    consent_announced_at = COALESCE(sqlc.narg(consent_announced_at), consent_announced_at),
    error = COALESCE(sqlc.narg(error), error)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteBotJob :execrows
DELETE FROM bot_jobs
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
