package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const countRecentCheckouts = `-- name: CountRecentCheckouts :one
SELECT count(*) FROM payments
WHERE workspace_id = $1 AND created_at >= $2
`

type CountRecentCheckoutsParams struct {
	WorkspaceID uuid.UUID          `json:"workspace_id"`
	Since       pgtype.Timestamptz `json:"since"`
}

func (q *Queries) CountRecentCheckouts(ctx context.Context, arg CountRecentCheckoutsParams) (int64, error) {
	row := q.db.QueryRow(ctx, countRecentCheckouts, arg.WorkspaceID, arg.Since)
	var count int64
	err := row.Scan(&count)
	return count, err
}

const createPayment = `-- name: CreatePayment :one
INSERT INTO payments (workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw)
VALUES ($1, $2, $3, $4,
        $5, $6, $7, $8)
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type CreatePaymentParams struct {
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Provider    string    `json:"provider"`
	ProviderRef string    `json:"provider_ref"`
	AmountMinor int64     `json:"amount_minor"`
	Currency    string    `json:"currency"`
	Minutes     int32     `json:"minutes"`
	Status      string    `json:"status"`
	Raw         []byte    `json:"raw"`
}

func (q *Queries) CreatePayment(ctx context.Context, arg CreatePaymentParams) (Payment, error) {
	row := q.db.QueryRow(ctx, createPayment,
		arg.WorkspaceID,
		arg.Provider,
		arg.ProviderRef,
		arg.AmountMinor,
		arg.Currency,
		arg.Minutes,
		arg.Status,
		arg.Raw,
	)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const failPendingPayment = `-- name: FailPendingPayment :execrows
UPDATE payments SET status = 'failed', raw = $1
WHERE id = $2 AND status = 'pending'
`

type FailPendingPaymentParams struct {
	Raw []byte    `json:"raw"`
	ID  uuid.UUID `json:"id"`
}

func (q *Queries) FailPendingPayment(ctx context.Context, arg FailPendingPaymentParams) (int64, error) {
	result, err := q.db.Exec(ctx, failPendingPayment, arg.Raw, arg.ID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const failStalePendingPayments = `-- name: FailStalePendingPayments :many
UPDATE payments SET
    status = 'failed',
    raw = raw || $1::jsonb
WHERE id IN (
    SELECT stale.id FROM payments AS stale
    WHERE stale.status = 'pending' AND stale.created_at < $2
    ORDER BY stale.created_at
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type FailStalePendingPaymentsParams struct {
	Reason    []byte             `json:"reason"`
	OlderThan pgtype.Timestamptz `json:"older_than"`
	RowLimit  int32              `json:"row_limit"`
}

func (q *Queries) FailStalePendingPayments(ctx context.Context, arg FailStalePendingPaymentsParams) ([]Payment, error) {
	rows, err := q.db.Query(ctx, failStalePendingPayments, arg.Reason, arg.OlderThan, arg.RowLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Payment{}
	for rows.Next() {
		var i Payment
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Provider,
			&i.ProviderRef,
			&i.AmountMinor,
			&i.Currency,
			&i.Minutes,
			&i.Status,
			&i.Raw,
			&i.CreatedAt,
			&i.PackID,
			&i.PaidAt,
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

const getPayment = `-- name: GetPayment :one
SELECT id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at FROM payments
WHERE id = $1 AND workspace_id = $2
`

type GetPaymentParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetPayment(ctx context.Context, arg GetPaymentParams) (Payment, error) {
	row := q.db.QueryRow(ctx, getPayment, arg.ID, arg.WorkspaceID)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const getPaymentByProviderRef = `-- name: GetPaymentByProviderRef :one
SELECT id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at FROM payments
WHERE provider = $1 AND provider_ref = $2
`

type GetPaymentByProviderRefParams struct {
	Provider    string `json:"provider"`
	ProviderRef string `json:"provider_ref"`
}

func (q *Queries) GetPaymentByProviderRef(ctx context.Context, arg GetPaymentByProviderRefParams) (Payment, error) {
	row := q.db.QueryRow(ctx, getPaymentByProviderRef, arg.Provider, arg.ProviderRef)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const insertPayment = `-- name: InsertPayment :one
INSERT INTO payments (id, workspace_id, pack_id, provider, provider_ref, amount_minor, currency, minutes, status, raw)
VALUES ($1, $2, $3, $4, $5,
        $6, $7, $8, 'pending', $9)
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type InsertPaymentParams struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	PackID      *uuid.UUID `json:"pack_id"`
	Provider    string     `json:"provider"`
	ProviderRef string     `json:"provider_ref"`
	AmountMinor int64      `json:"amount_minor"`
	Currency    string     `json:"currency"`
	Minutes     int32      `json:"minutes"`
	Raw         []byte     `json:"raw"`
}

func (q *Queries) InsertPayment(ctx context.Context, arg InsertPaymentParams) (Payment, error) {
	row := q.db.QueryRow(ctx, insertPayment,
		arg.ID,
		arg.WorkspaceID,
		arg.PackID,
		arg.Provider,
		arg.ProviderRef,
		arg.AmountMinor,
		arg.Currency,
		arg.Minutes,
		arg.Raw,
	)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const listPaymentsByWorkspace = `-- name: ListPaymentsByWorkspace :many
SELECT id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at FROM payments
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListPaymentsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListPaymentsByWorkspace(ctx context.Context, arg ListPaymentsByWorkspaceParams) ([]Payment, error) {
	rows, err := q.db.Query(ctx, listPaymentsByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Payment{}
	for rows.Next() {
		var i Payment
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Provider,
			&i.ProviderRef,
			&i.AmountMinor,
			&i.Currency,
			&i.Minutes,
			&i.Status,
			&i.Raw,
			&i.CreatedAt,
			&i.PackID,
			&i.PaidAt,
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

const lockPaymentByID = `-- name: LockPaymentByID :one
SELECT id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at FROM payments
WHERE provider = $1 AND id = $2
FOR UPDATE
`

type LockPaymentByIDParams struct {
	Provider string    `json:"provider"`
	ID       uuid.UUID `json:"id"`
}

func (q *Queries) LockPaymentByID(ctx context.Context, arg LockPaymentByIDParams) (Payment, error) {
	row := q.db.QueryRow(ctx, lockPaymentByID, arg.Provider, arg.ID)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const lockPaymentByProviderRef = `-- name: LockPaymentByProviderRef :one
SELECT id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at FROM payments
WHERE provider = $1 AND provider_ref = $2
FOR UPDATE
`

type LockPaymentByProviderRefParams struct {
	Provider    string `json:"provider"`
	ProviderRef string `json:"provider_ref"`
}

func (q *Queries) LockPaymentByProviderRef(ctx context.Context, arg LockPaymentByProviderRefParams) (Payment, error) {
	row := q.db.QueryRow(ctx, lockPaymentByProviderRef, arg.Provider, arg.ProviderRef)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const lockWorkspaceCheckouts = `-- name: LockWorkspaceCheckouts :exec
SELECT pg_advisory_xact_lock(72101, hashtext($1::text))
`

func (q *Queries) LockWorkspaceCheckouts(ctx context.Context, workspaceID string) error {
	_, err := q.db.Exec(ctx, lockWorkspaceCheckouts, workspaceID)
	return err
}

const markPaymentNeedsReview = `-- name: MarkPaymentNeedsReview :one
UPDATE payments SET
    status = 'needs_review',
    raw = raw || $1::jsonb
WHERE id = $2 AND status <> 'paid'
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type MarkPaymentNeedsReviewParams struct {
	Reason []byte    `json:"reason"`
	ID     uuid.UUID `json:"id"`
}

func (q *Queries) MarkPaymentNeedsReview(ctx context.Context, arg MarkPaymentNeedsReviewParams) (Payment, error) {
	row := q.db.QueryRow(ctx, markPaymentNeedsReview, arg.Reason, arg.ID)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const setPaymentProviderRef = `-- name: SetPaymentProviderRef :one
UPDATE payments SET provider_ref = $1
WHERE id = $2 AND status = 'pending'
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type SetPaymentProviderRefParams struct {
	ProviderRef string    `json:"provider_ref"`
	ID          uuid.UUID `json:"id"`
}

func (q *Queries) SetPaymentProviderRef(ctx context.Context, arg SetPaymentProviderRefParams) (Payment, error) {
	row := q.db.QueryRow(ctx, setPaymentProviderRef, arg.ProviderRef, arg.ID)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}

const settlePayment = `-- name: SettlePayment :one
UPDATE payments SET
    status = $1,
    paid_at = $2,
    raw = $3
WHERE id = $4
  AND status = 'pending'
  AND $1 IN ('paid', 'failed')
RETURNING id, workspace_id, provider, provider_ref, amount_minor, currency, minutes, status, raw, created_at, pack_id, paid_at
`

type SettlePaymentParams struct {
	Status string             `json:"status"`
	PaidAt pgtype.Timestamptz `json:"paid_at"`
	Raw    []byte             `json:"raw"`
	ID     uuid.UUID          `json:"id"`
}

func (q *Queries) SettlePayment(ctx context.Context, arg SettlePaymentParams) (Payment, error) {
	row := q.db.QueryRow(ctx, settlePayment,
		arg.Status,
		arg.PaidAt,
		arg.Raw,
		arg.ID,
	)
	var i Payment
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Provider,
		&i.ProviderRef,
		&i.AmountMinor,
		&i.Currency,
		&i.Minutes,
		&i.Status,
		&i.Raw,
		&i.CreatedAt,
		&i.PackID,
		&i.PaidAt,
	)
	return i, err
}
