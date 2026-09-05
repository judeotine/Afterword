-- name: GetActiveCreditGrant :one
SELECT * FROM credit_grants
WHERE workspace_id = sqlc.arg(workspace_id) AND expired_at IS NULL
ORDER BY period DESC
LIMIT 1;

-- name: GetCreditGrantForPeriod :one
SELECT * FROM credit_grants
WHERE workspace_id = sqlc.arg(workspace_id) AND period = sqlc.arg(period);

-- name: ListCreditGrantsByWorkspace :many
SELECT * FROM credit_grants
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);
