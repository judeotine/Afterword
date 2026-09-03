-- name: CreateWorkspaceInvite :one
INSERT INTO workspace_invites (workspace_id, email, role, token_hash, invited_by_user_id, expires_at)
VALUES (
    sqlc.arg(workspace_id),
    sqlc.arg(email),
    sqlc.arg(role),
    sqlc.arg(token_hash),
    sqlc.narg(invited_by_user_id),
    sqlc.arg(expires_at)
)
RETURNING *;

-- name: GetWorkspaceInviteByTokenHash :one
SELECT * FROM workspace_invites WHERE token_hash = sqlc.arg(token_hash);

-- name: AcceptWorkspaceInvite :one
UPDATE workspace_invites
SET accepted_at = sqlc.arg(accepted_at), accepted_by_user_id = sqlc.arg(accepted_by_user_id)
WHERE id = sqlc.arg(id) AND accepted_at IS NULL AND revoked_at IS NULL
RETURNING *;

-- name: RevokeExpiredWorkspaceInvites :execrows
UPDATE workspace_invites SET revoked_at = sqlc.arg(revoked_at)
WHERE workspace_id = sqlc.arg(workspace_id)
  AND email = sqlc.arg(email)
  AND accepted_at IS NULL
  AND revoked_at IS NULL
  AND expires_at <= sqlc.arg(revoked_at);

-- name: RevokeWorkspaceInvite :execrows
UPDATE workspace_invites SET revoked_at = sqlc.arg(revoked_at)
WHERE id = sqlc.arg(id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND accepted_at IS NULL
  AND revoked_at IS NULL;

-- name: ListWorkspaceInvites :many
SELECT * FROM workspace_invites
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);
