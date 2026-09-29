package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createCreditPack = `-- name: CreateCreditPack :one
INSERT INTO credit_packs (name, minutes, price_minor, currency, active)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, minutes, price_minor, currency, active, created_at
`

type CreateCreditPackParams struct {
	Name       string `json:"name"`
	Minutes    int32  `json:"minutes"`
	PriceMinor int64  `json:"price_minor"`
	Currency   string `json:"currency"`
	Active     bool   `json:"active"`
}

func (q *Queries) CreateCreditPack(ctx context.Context, arg CreateCreditPackParams) (CreditPack, error) {
	row := q.db.QueryRow(ctx, createCreditPack,
		arg.Name,
		arg.Minutes,
		arg.PriceMinor,
		arg.Currency,
		arg.Active,
	)
	var i CreditPack
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Minutes,
		&i.PriceMinor,
		&i.Currency,
		&i.Active,
		&i.CreatedAt,
	)
	return i, err
}

const deleteCreditPack = `-- name: DeleteCreditPack :execrows
DELETE FROM credit_packs WHERE id = $1
`

func (q *Queries) DeleteCreditPack(ctx context.Context, id uuid.UUID) (int64, error) {
	result, err := q.db.Exec(ctx, deleteCreditPack, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getActiveCreditPack = `-- name: GetActiveCreditPack :one
SELECT id, name, minutes, price_minor, currency, active, created_at FROM credit_packs
WHERE id = $1 AND active
`

func (q *Queries) GetActiveCreditPack(ctx context.Context, id uuid.UUID) (CreditPack, error) {
	row := q.db.QueryRow(ctx, getActiveCreditPack, id)
	var i CreditPack
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Minutes,
		&i.PriceMinor,
		&i.Currency,
		&i.Active,
		&i.CreatedAt,
	)
	return i, err
}

const getCreditPack = `-- name: GetCreditPack :one
SELECT id, name, minutes, price_minor, currency, active, created_at FROM credit_packs WHERE id = $1
`

func (q *Queries) GetCreditPack(ctx context.Context, id uuid.UUID) (CreditPack, error) {
	row := q.db.QueryRow(ctx, getCreditPack, id)
	var i CreditPack
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Minutes,
		&i.PriceMinor,
		&i.Currency,
		&i.Active,
		&i.CreatedAt,
	)
	return i, err
}

const listActiveCreditPacks = `-- name: ListActiveCreditPacks :many
SELECT id, name, minutes, price_minor, currency, active, created_at FROM credit_packs
WHERE active
ORDER BY minutes, id
`

func (q *Queries) ListActiveCreditPacks(ctx context.Context) ([]CreditPack, error) {
	rows, err := q.db.Query(ctx, listActiveCreditPacks)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CreditPack{}
	for rows.Next() {
		var i CreditPack
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.Minutes,
			&i.PriceMinor,
			&i.Currency,
			&i.Active,
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

const listCreditPacks = `-- name: ListCreditPacks :many
SELECT id, name, minutes, price_minor, currency, active, created_at FROM credit_packs
WHERE ($1::boolean IS NULL OR active = $1::boolean)
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListCreditPacksParams struct {
	Active          *bool              `json:"active"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListCreditPacks(ctx context.Context, arg ListCreditPacksParams) ([]CreditPack, error) {
	rows, err := q.db.Query(ctx, listCreditPacks,
		arg.Active,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CreditPack{}
	for rows.Next() {
		var i CreditPack
		if err := rows.Scan(
			&i.ID,
			&i.Name,
			&i.Minutes,
			&i.PriceMinor,
			&i.Currency,
			&i.Active,
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

const updateCreditPack = `-- name: UpdateCreditPack :one
UPDATE credit_packs SET
    name = COALESCE($1, name),
    minutes = COALESCE($2, minutes),
    price_minor = COALESCE($3, price_minor),
    currency = COALESCE($4, currency),
    active = COALESCE($5, active)
WHERE id = $6
RETURNING id, name, minutes, price_minor, currency, active, created_at
`

type UpdateCreditPackParams struct {
	Name       *string   `json:"name"`
	Minutes    *int32    `json:"minutes"`
	PriceMinor *int64    `json:"price_minor"`
	Currency   *string   `json:"currency"`
	Active     *bool     `json:"active"`
	ID         uuid.UUID `json:"id"`
}

func (q *Queries) UpdateCreditPack(ctx context.Context, arg UpdateCreditPackParams) (CreditPack, error) {
	row := q.db.QueryRow(ctx, updateCreditPack,
		arg.Name,
		arg.Minutes,
		arg.PriceMinor,
		arg.Currency,
		arg.Active,
		arg.ID,
	)
	var i CreditPack
	err := row.Scan(
		&i.ID,
		&i.Name,
		&i.Minutes,
		&i.PriceMinor,
		&i.Currency,
		&i.Active,
		&i.CreatedAt,
	)
	return i, err
}
