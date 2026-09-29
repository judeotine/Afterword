package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const ensureCreditLock = `-- name: EnsureCreditLock :exec
INSERT INTO credit_locks (workspace_id) VALUES ($1)
ON CONFLICT (workspace_id) DO NOTHING
`

func (q *Queries) EnsureCreditLock(ctx context.Context, workspaceID uuid.UUID) error {
	_, err := q.db.Exec(ctx, ensureCreditLock, workspaceID)
	return err
}

const getCreditBalance = `-- name: GetCreditBalance :one
SELECT COALESCE(SUM(delta_minutes), 0)::bigint AS balance_minutes
FROM credit_ledger
WHERE workspace_id = $1
`

func (q *Queries) GetCreditBalance(ctx context.Context, workspaceID uuid.UUID) (int64, error) {
	row := q.db.QueryRow(ctx, getCreditBalance, workspaceID)
	var balance_minutes int64
	err := row.Scan(&balance_minutes)
	return balance_minutes, err
}

const getCreditLedgerEntry = `-- name: GetCreditLedgerEntry :one
SELECT id, workspace_id, delta_minutes, reason, ref_id, created_at FROM credit_ledger
WHERE id = $1 AND workspace_id = $2
`

type GetCreditLedgerEntryParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetCreditLedgerEntry(ctx context.Context, arg GetCreditLedgerEntryParams) (CreditLedger, error) {
	row := q.db.QueryRow(ctx, getCreditLedgerEntry, arg.ID, arg.WorkspaceID)
	var i CreditLedger
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.DeltaMinutes,
		&i.Reason,
		&i.RefID,
		&i.CreatedAt,
	)
	return i, err
}

const listCreditLedgerByWorkspace = `-- name: ListCreditLedgerByWorkspace :many
SELECT id, workspace_id, delta_minutes, reason, ref_id, created_at FROM credit_ledger
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListCreditLedgerByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListCreditLedgerByWorkspace(ctx context.Context, arg ListCreditLedgerByWorkspaceParams) ([]CreditLedger, error) {
	rows, err := q.db.Query(ctx, listCreditLedgerByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CreditLedger{}
	for rows.Next() {
		var i CreditLedger
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.DeltaMinutes,
			&i.Reason,
			&i.RefID,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const lockCreditWorkspace = `-- name: LockCreditWorkspace :one
SELECT workspace_id FROM credit_locks
WHERE workspace_id = $1
FOR UPDATE
`

func (q *Queries) LockCreditWorkspace(ctx context.Context, workspaceID uuid.UUID) (uuid.UUID, error) {
	row := q.db.QueryRow(ctx, lockCreditWorkspace, workspaceID)
	var workspace_id uuid.UUID
	err := row.Scan(&workspace_id)
	return workspace_id, err
}
