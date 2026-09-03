-- name: CreateJob :one
INSERT INTO jobs (kind, payload, idempotency_key, run_at, max_attempts)
VALUES (sqlc.arg(kind), sqlc.arg(payload), sqlc.narg(idempotency_key), sqlc.arg(run_at), sqlc.arg(max_attempts))
ON CONFLICT (kind, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = sqlc.arg(id);

-- name: GetJobByIdempotencyKey :one
SELECT * FROM jobs
WHERE kind = sqlc.arg(kind) AND idempotency_key = sqlc.arg(idempotency_key);

-- name: ListJobs :many
SELECT * FROM jobs
WHERE (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateJob :one
UPDATE jobs SET
    payload = COALESCE(sqlc.narg(payload), payload),
    run_at = COALESCE(sqlc.narg(run_at), run_at),
    max_attempts = COALESCE(sqlc.narg(max_attempts), max_attempts),
    updated_at = now()
WHERE id = sqlc.arg(id) AND status <> 'running'
RETURNING *;

-- name: DeleteJob :execrows
DELETE FROM jobs WHERE id = sqlc.arg(id);
