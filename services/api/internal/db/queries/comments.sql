-- name: CreateComment :one
INSERT INTO comments (meeting_id, user_id, at_s, body)
SELECT sqlc.arg(meeting_id), sqlc.narg(user_id), sqlc.arg(at_s), sqlc.arg(body)
FROM meetings
WHERE meetings.id = sqlc.arg(meeting_id) AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: GetComment :one
SELECT comments.* FROM comments
JOIN meetings ON meetings.id = comments.meeting_id
WHERE comments.id = sqlc.arg(id) AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListCommentsByWorkspace :many
SELECT comments.* FROM comments
JOIN meetings ON meetings.id = comments.meeting_id
WHERE meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(meeting_id)::uuid IS NULL OR comments.meeting_id = sqlc.narg(meeting_id)::uuid)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (comments.created_at, comments.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY comments.created_at DESC, comments.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateComment :one
UPDATE comments SET
    at_s = COALESCE(sqlc.narg(at_s), at_s),
    body = COALESCE(sqlc.narg(body), body)
FROM meetings
WHERE comments.meeting_id = meetings.id
  AND comments.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING comments.*;

-- name: DeleteComment :execrows
DELETE FROM comments
USING meetings
WHERE comments.meeting_id = meetings.id
  AND comments.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
