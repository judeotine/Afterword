-- name: CreateCreditLedgerEntry :one
INSERT INTO credit_ledger (workspace_id, delta_minutes, reason, ref_id)
VALUES (sqlc.arg(workspace_id), sqlc.arg(delta_minutes), sqlc.arg(reason), sqlc.narg(ref_id))
RETURNING *;

-- name: GetCreditLedgerEntry :one
SELECT * FROM credit_ledger
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListCreditLedgerByWorkspace :many
SELECT * FROM credit_ledger
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: GetCreditBalance :one
SELECT COALESCE(SUM(delta_minutes), 0)::bigint AS balance_minutes
FROM credit_ledger
WHERE workspace_id = sqlc.arg(workspace_id);

-- name: UpdateCreditLedgerEntry :one
UPDATE credit_ledger SET ref_id = COALESCE(sqlc.narg(ref_id), ref_id)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteCreditLedgerEntry :execrows
DELETE FROM credit_ledger
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: EnsureCreditLock :exec
INSERT INTO credit_locks (workspace_id) VALUES (sqlc.arg(workspace_id))
ON CONFLICT (workspace_id) DO NOTHING;

-- name: LockCreditWorkspace :one
SELECT workspace_id FROM credit_locks
WHERE workspace_id = sqlc.arg(workspace_id)
FOR UPDATE;
