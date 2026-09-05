-- name: CreateShareLink :one
INSERT INTO share_links (meeting_id, token_hash, permission, expires_at)
SELECT sqlc.arg(meeting_id), sqlc.arg(token_hash), sqlc.arg(permission), sqlc.narg(expires_at)
FROM meetings
WHERE meetings.id = sqlc.arg(meeting_id) AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: GetShareLink :one
SELECT share_links.* FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE share_links.id = sqlc.arg(id) AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: GetShareLinkByTokenHash :one
SELECT * FROM share_links WHERE token_hash = sqlc.arg(token_hash);

-- name: ListShareLinksByWorkspace :many
SELECT share_links.* FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(meeting_id)::uuid IS NULL OR share_links.meeting_id = sqlc.narg(meeting_id)::uuid)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (share_links.created_at, share_links.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY share_links.created_at DESC, share_links.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateShareLink :one
UPDATE share_links SET
    permission = COALESCE(sqlc.narg(permission), permission),
    expires_at = COALESCE(sqlc.narg(expires_at), expires_at)
FROM meetings
WHERE share_links.meeting_id = meetings.id
  AND share_links.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING share_links.*;

-- name: DeleteShareLink :execrows
DELETE FROM share_links
USING meetings
WHERE share_links.meeting_id = meetings.id
  AND share_links.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
