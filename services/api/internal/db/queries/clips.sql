-- name: CreateClip :one
INSERT INTO clips (meeting_id, start_s, end_s, title, object, share_token)
SELECT sqlc.arg(meeting_id), sqlc.arg(start_s), sqlc.arg(end_s), sqlc.arg(title),
       sqlc.narg(object), sqlc.narg(share_token)
FROM meetings
WHERE meetings.id = sqlc.arg(meeting_id) AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: GetClip :one
SELECT clips.* FROM clips
JOIN meetings ON meetings.id = clips.meeting_id
WHERE clips.id = sqlc.arg(id) AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListClipsByWorkspace :many
SELECT clips.* FROM clips
JOIN meetings ON meetings.id = clips.meeting_id
WHERE meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(meeting_id)::uuid IS NULL OR clips.meeting_id = sqlc.narg(meeting_id)::uuid)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (clips.created_at, clips.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY clips.created_at DESC, clips.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateClip :one
UPDATE clips SET
    start_s = COALESCE(sqlc.narg(start_s), clips.start_s),
    end_s = COALESCE(sqlc.narg(end_s), clips.end_s),
    title = COALESCE(sqlc.narg(title), clips.title),
    object = COALESCE(sqlc.narg(object), clips.object),
    share_token = COALESCE(sqlc.narg(share_token), clips.share_token)
FROM meetings
WHERE clips.meeting_id = meetings.id
  AND clips.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING clips.*;

-- name: DeleteClip :execrows
DELETE FROM clips
USING meetings
WHERE clips.meeting_id = meetings.id
  AND clips.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
