-- name: CreatePayment :one
INSERT INTO payments (workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw)
VALUES (sqlc.arg(workspace_id), sqlc.arg(provider), sqlc.arg(provider_ref), sqlc.arg(amount_minor),
        sqlc.arg(currency), sqlc.arg(minutes), sqlc.arg(status), sqlc.arg(raw))
RETURNING *;

-- name: GetPayment :one
SELECT * FROM payments
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: GetPaymentByProviderRef :one
SELECT * FROM payments
WHERE provider = sqlc.arg(provider) AND provider_ref = sqlc.arg(provider_ref);

-- name: ListPaymentsByWorkspace :many
SELECT * FROM payments
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdatePayment :one
UPDATE payments SET
    status = COALESCE(sqlc.narg(status), status),
    minutes = COALESCE(sqlc.narg(minutes), minutes),
    raw = COALESCE(sqlc.narg(raw), raw)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeletePayment :execrows
DELETE FROM payments
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
