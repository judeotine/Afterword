package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const getActiveCreditGrant = `-- name: GetActiveCreditGrant :one
SELECT id, workspace_id, period, minutes, expires_at, expired_at, expired_minutes, created_at FROM credit_grants
WHERE workspace_id = $1 AND expired_at IS NULL
ORDER BY period DESC
LIMIT 1
`

func (q *Queries) GetActiveCreditGrant(ctx context.Context, workspaceID uuid.UUID) (CreditGrant, error) {
	row := q.db.QueryRow(ctx, getActiveCreditGrant, workspaceID)
	var i CreditGrant
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Period,
		&i.Minutes,
		&i.ExpiresAt,
		&i.ExpiredAt,
		&i.ExpiredMinutes,
		&i.CreatedAt,
	)
	return i, err
}

const getCreditGrantForPeriod = `-- name: GetCreditGrantForPeriod :one
SELECT id, workspace_id, period, minutes, expires_at, expired_at, expired_minutes, created_at FROM credit_grants
WHERE workspace_id = $1 AND period = $2
`

type GetCreditGrantForPeriodParams struct {
	WorkspaceID uuid.UUID   `json:"workspace_id"`
	Period      pgtype.Date `json:"period"`
}

func (q *Queries) GetCreditGrantForPeriod(ctx context.Context, arg GetCreditGrantForPeriodParams) (CreditGrant, error) {
	row := q.db.QueryRow(ctx, getCreditGrantForPeriod, arg.WorkspaceID, arg.Period)
	var i CreditGrant
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.Period,
		&i.Minutes,
		&i.ExpiresAt,
		&i.ExpiredAt,
		&i.ExpiredMinutes,
		&i.CreatedAt,
	)
	return i, err
}

const listCreditGrantsByWorkspace = `-- name: ListCreditGrantsByWorkspace :many
SELECT id, workspace_id, period, minutes, expires_at, expired_at, expired_minutes, created_at FROM credit_grants
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListCreditGrantsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListCreditGrantsByWorkspace(ctx context.Context, arg ListCreditGrantsByWorkspaceParams) ([]CreditGrant, error) {
	rows, err := q.db.Query(ctx, listCreditGrantsByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []CreditGrant{}
	for rows.Next() {
		var i CreditGrant
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.Period,
			&i.Minutes,
			&i.ExpiresAt,
			&i.ExpiredAt,
			&i.ExpiredMinutes,
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
