-- name: CreateMeeting :one
INSERT INTO meetings (
    workspace_id, owner_user_id, title, source, platform, started_at, duration_s,
    consent_state, visibility, folder_id, audio_object, transcript_object, status
) VALUES (
    sqlc.arg(workspace_id), sqlc.narg(owner_user_id), sqlc.arg(title), sqlc.arg(source),
    sqlc.narg(platform), sqlc.narg(started_at), sqlc.arg(duration_s), sqlc.arg(consent_state),
    sqlc.arg(visibility), sqlc.narg(folder_id), sqlc.narg(audio_object),
    sqlc.narg(transcript_object), sqlc.arg(status)
)
RETURNING *;

-- name: GetMeeting :one
SELECT * FROM meetings
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);

-- name: ListMeetingsByWorkspace :many
SELECT * FROM meetings
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateMeeting :one
UPDATE meetings SET
    title = COALESCE(sqlc.narg(title), title),
    platform = COALESCE(sqlc.narg(platform), platform),
    started_at = COALESCE(sqlc.narg(started_at), started_at),
    duration_s = COALESCE(sqlc.narg(duration_s), duration_s),
    consent_state = COALESCE(sqlc.narg(consent_state), consent_state),
    visibility = COALESCE(sqlc.narg(visibility), visibility),
    folder_id = COALESCE(sqlc.narg(folder_id), folder_id),
    audio_object = COALESCE(sqlc.narg(audio_object), audio_object),
    transcript_object = COALESCE(sqlc.narg(transcript_object), transcript_object),
    status = COALESCE(sqlc.narg(status), status)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteMeeting :execrows
DELETE FROM meetings
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id);
