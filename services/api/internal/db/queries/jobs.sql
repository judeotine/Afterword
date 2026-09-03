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

-- name: ClaimJob :one
UPDATE jobs SET
    status = 'running',
    locked_by = sqlc.arg(worker_id),
    locked_at = now(),
    attempts = attempts + 1,
    updated_at = now()
WHERE id = (
    SELECT id FROM jobs
    WHERE status = 'pending'
      AND run_at <= now()
      AND kind = ANY (sqlc.arg(kinds)::text[])
    ORDER BY run_at, id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING *;

-- name: CompleteJob :one
UPDATE jobs SET
    status = 'succeeded',
    locked_by = NULL,
    locked_at = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING *;

-- name: FailJob :one
UPDATE jobs SET
    status = CASE WHEN attempts >= max_attempts THEN 'dead' ELSE 'pending' END,
    run_at = CASE WHEN attempts >= max_attempts THEN run_at ELSE sqlc.arg(retry_at) END,
    locked_by = NULL,
    locked_at = NULL,
    last_error = sqlc.arg(last_error),
    updated_at = now()
WHERE id = sqlc.arg(id) AND status = 'running'
RETURNING *;

-- name: ReleaseStaleJobs :many
UPDATE jobs SET
    status = 'pending',
    locked_by = NULL,
    locked_at = NULL,
    updated_at = now()
WHERE status = 'running' AND locked_at < sqlc.arg(older_than)
RETURNING *;

-- name: UpdateJob :one
UPDATE jobs SET
    payload = COALESCE(sqlc.narg(payload), payload),
    run_at = COALESCE(sqlc.narg(run_at), run_at),
    status = COALESCE(sqlc.narg(status), status),
    max_attempts = COALESCE(sqlc.narg(max_attempts), max_attempts),
    last_error = COALESCE(sqlc.narg(last_error), last_error),
    updated_at = now()
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteJob :execrows
DELETE FROM jobs WHERE id = sqlc.arg(id);
