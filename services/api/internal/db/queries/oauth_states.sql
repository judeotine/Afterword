-- name: CreateOAuthState :one
INSERT INTO oauth_states (provider, state_hash, nonce_hash, code_verifier, redirect_to, expires_at, created_at)
VALUES (
    sqlc.arg(provider),
    sqlc.arg(state_hash),
    sqlc.arg(nonce_hash),
    sqlc.arg(code_verifier),
    sqlc.arg(redirect_to),
    sqlc.arg(expires_at),
    sqlc.arg(created_at)
)
RETURNING *;

-- name: ConsumeOAuthState :one
UPDATE oauth_states SET consumed_at = sqlc.arg(consumed_at)
WHERE state_hash = sqlc.arg(state_hash)
  AND provider = sqlc.arg(provider)
  AND consumed_at IS NULL
RETURNING *;

-- name: DeleteExpiredOAuthStates :execrows
DELETE FROM oauth_states WHERE expires_at < sqlc.arg(before);
