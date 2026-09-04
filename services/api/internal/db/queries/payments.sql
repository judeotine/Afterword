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

-- name: InsertPayment :one
INSERT INTO payments (id, workspace_id, pack_id, provider, provider_ref, amount_minor, currency, minutes, status, raw)
VALUES (sqlc.arg(id), sqlc.arg(workspace_id), sqlc.narg(pack_id), sqlc.arg(provider), sqlc.arg(provider_ref),
        sqlc.arg(amount_minor), sqlc.arg(currency), sqlc.arg(minutes), 'pending', sqlc.arg(raw))
RETURNING *;

-- name: SetPaymentProviderRef :one
UPDATE payments SET provider_ref = sqlc.arg(provider_ref)
WHERE id = sqlc.arg(id) AND status = 'pending'
RETURNING *;

-- name: LockPaymentByProviderRef :one
SELECT * FROM payments
WHERE provider = sqlc.arg(provider) AND provider_ref = sqlc.arg(provider_ref)
FOR UPDATE;

-- name: SettlePayment :one
UPDATE payments SET
    status = sqlc.arg(status),
    paid_at = sqlc.narg(paid_at),
    raw = sqlc.arg(raw)
WHERE id = sqlc.arg(id)
  AND status = 'pending'
  AND sqlc.arg(status) IN ('paid', 'failed')
RETURNING *;

-- name: FailPendingPayment :execrows
UPDATE payments SET status = 'failed', raw = sqlc.arg(raw)
WHERE id = sqlc.arg(id) AND status = 'pending';

-- name: FailStalePendingPayments :many
UPDATE payments SET
    status = 'failed',
    raw = raw || sqlc.arg(reason)::jsonb
WHERE status = 'pending' AND created_at < sqlc.arg(older_than)
RETURNING *;

-- name: MarkPaymentNeedsReview :one
UPDATE payments SET
    status = 'needs_review',
    raw = raw || sqlc.arg(reason)::jsonb
WHERE id = sqlc.arg(id) AND status <> 'paid'
RETURNING *;

-- name: LockWorkspaceCheckouts :exec
SELECT pg_advisory_xact_lock(72101, hashtext(sqlc.arg(workspace_id)::text));

-- name: CountRecentCheckouts :one
SELECT count(*) FROM payments
WHERE workspace_id = sqlc.arg(workspace_id) AND created_at >= sqlc.arg(since);
