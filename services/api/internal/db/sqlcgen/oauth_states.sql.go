package sqlcgen

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const consumeOAuthState = `-- name: ConsumeOAuthState :one
UPDATE oauth_states SET consumed_at = $1
WHERE state_hash = $2
  AND provider = $3
  AND consumed_at IS NULL
RETURNING id, provider, state_hash, code_verifier, redirect_to, expires_at, consumed_at, created_at, nonce_hash
`

type ConsumeOAuthStateParams struct {
	ConsumedAt pgtype.Timestamptz `json:"consumed_at"`
	StateHash  string             `json:"state_hash"`
	Provider   string             `json:"provider"`
}

func (q *Queries) ConsumeOAuthState(ctx context.Context, arg ConsumeOAuthStateParams) (OauthState, error) {
	row := q.db.QueryRow(ctx, consumeOAuthState, arg.ConsumedAt, arg.StateHash, arg.Provider)
	var i OauthState
	err := row.Scan(
		&i.ID,
		&i.Provider,
		&i.StateHash,
		&i.CodeVerifier,
		&i.RedirectTo,
		&i.ExpiresAt,
		&i.ConsumedAt,
		&i.CreatedAt,
		&i.NonceHash,
	)
	return i, err
}

const createOAuthState = `-- name: CreateOAuthState :one
INSERT INTO oauth_states (provider, state_hash, nonce_hash, code_verifier, redirect_to, expires_at, created_at)
VALUES (
    $1,
    $2,
    $3,
    $4,
    $5,
    $6,
    $7
)
RETURNING id, provider, state_hash, code_verifier, redirect_to, expires_at, consumed_at, created_at, nonce_hash
`

type CreateOAuthStateParams struct {
	Provider     string             `json:"provider"`
	StateHash    string             `json:"state_hash"`
	NonceHash    string             `json:"nonce_hash"`
	CodeVerifier string             `json:"code_verifier"`
	RedirectTo   string             `json:"redirect_to"`
	ExpiresAt    pgtype.Timestamptz `json:"expires_at"`
	CreatedAt    pgtype.Timestamptz `json:"created_at"`
}

func (q *Queries) CreateOAuthState(ctx context.Context, arg CreateOAuthStateParams) (OauthState, error) {
	row := q.db.QueryRow(ctx, createOAuthState,
		arg.Provider,
		arg.StateHash,
		arg.NonceHash,
		arg.CodeVerifier,
		arg.RedirectTo,
		arg.ExpiresAt,
		arg.CreatedAt,
	)
	var i OauthState
	err := row.Scan(
		&i.ID,
		&i.Provider,
		&i.StateHash,
		&i.CodeVerifier,
		&i.RedirectTo,
		&i.ExpiresAt,
		&i.ConsumedAt,
		&i.CreatedAt,
		&i.NonceHash,
	)
	return i, err
}

const deleteExpiredOAuthStates = `-- name: DeleteExpiredOAuthStates :execrows
DELETE FROM oauth_states WHERE expires_at < $1
`

func (q *Queries) DeleteExpiredOAuthStates(ctx context.Context, before pgtype.Timestamptz) (int64, error) {
	result, err := q.db.Exec(ctx, deleteExpiredOAuthStates, before)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
