-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, family_id, token_hash, device_id, expires_at, created_at)
VALUES (
    sqlc.arg(user_id),
    sqlc.arg(family_id),
    sqlc.arg(token_hash),
    sqlc.arg(device_id),
    sqlc.arg(expires_at),
    sqlc.arg(created_at)
)
RETURNING *;

-- name: GetRefreshTokenByHash :one
SELECT * FROM refresh_tokens WHERE token_hash = sqlc.arg(token_hash);

-- name: MarkRefreshTokenUsed :execrows
UPDATE refresh_tokens
SET revoked_at = sqlc.arg(used_at), last_used_at = sqlc.arg(used_at)
WHERE id = sqlc.arg(id) AND revoked_at IS NULL;

-- name: RevokeRefreshFamily :execrows
UPDATE refresh_tokens SET revoked_at = sqlc.arg(revoked_at)
WHERE family_id = sqlc.arg(family_id) AND revoked_at IS NULL;

-- name: RevokeUserRefreshTokens :execrows
UPDATE refresh_tokens SET revoked_at = sqlc.arg(revoked_at)
WHERE user_id = sqlc.arg(user_id) AND revoked_at IS NULL;

-- name: ListRefreshTokensByUser :many
SELECT * FROM refresh_tokens
WHERE user_id = sqlc.arg(user_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: DeleteExpiredRefreshTokens :execrows
DELETE FROM refresh_tokens WHERE expires_at < sqlc.arg(before);
