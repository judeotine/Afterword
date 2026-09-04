-- name: ListMeetingsPage :many
SELECT * FROM meetings
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (visibility <> 'private' OR owner_user_id = sqlc.arg(viewer_user_id))
  AND (sqlc.narg(folder_id)::uuid IS NULL OR folder_id = sqlc.narg(folder_id)::uuid)
  AND (sqlc.narg(source)::text IS NULL OR source = sqlc.narg(source)::text)
  AND (sqlc.narg(from_time)::timestamptz IS NULL OR COALESCE(started_at, created_at) >= sqlc.narg(from_time)::timestamptz)
  AND (sqlc.narg(to_time)::timestamptz IS NULL OR COALESCE(started_at, created_at) <= sqlc.narg(to_time)::timestamptz)
  AND (sqlc.narg(search)::text IS NULL OR title ILIKE '%' || sqlc.narg(search)::text || '%' ESCAPE '\')
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateMeetingDetails :one
UPDATE meetings SET
    title = COALESCE(sqlc.narg(title), title),
    visibility = COALESCE(sqlc.narg(visibility), visibility),
    folder_id = CASE
        WHEN sqlc.arg(clear_folder)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(folder_id), folder_id)
    END
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: FinalizeMeetingObjects :one
UPDATE meetings SET
    status = sqlc.arg(status),
    audio_bytes = sqlc.narg(audio_bytes),
    transcript_bytes = sqlc.narg(transcript_bytes),
    audio_etag = sqlc.narg(audio_etag),
    transcript_etag = sqlc.narg(transcript_etag),
    duration_s = COALESCE(sqlc.narg(duration_s), duration_s),
    finalize_generation = sqlc.arg(finalize_generation)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: SetMeetingLinkSharing :one
UPDATE meetings SET link_sharing_enabled = sqlc.arg(link_sharing_enabled)
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: LockMeetingForPurge :one
SELECT id FROM meetings
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
FOR UPDATE;

-- name: ListClipObjectsForMeeting :many
SELECT clips.object FROM clips
JOIN meetings ON meetings.id = clips.meeting_id
WHERE clips.meeting_id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
  AND clips.object IS NOT NULL;

-- name: TryRecordShareLinkRequest :one
INSERT INTO share_link_requests (request_ip, created_at)
SELECT sqlc.arg(request_ip), sqlc.arg(recorded_at)
WHERE (
    SELECT count(*) FROM share_link_requests AS recent
    WHERE recent.request_ip = sqlc.arg(request_ip) AND recent.created_at >= sqlc.arg(since)
) < sqlc.arg(request_limit)::bigint
RETURNING id;

-- name: LockShareLinkIP :exec
SELECT pg_advisory_xact_lock(hashtext('share-ip:' || sqlc.arg(request_ip)::text));

-- name: DeleteExpiredShareLinkRequests :execrows
DELETE FROM share_link_requests WHERE created_at < sqlc.arg(before);

-- name: DeleteMeetingReturning :one
DELETE FROM meetings
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteSegmentsForMeeting :execrows
DELETE FROM transcript_segments
USING meetings
WHERE transcript_segments.meeting_id = meetings.id
  AND meetings.id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListSegmentsBySeq :many
SELECT transcript_segments.id, transcript_segments.meeting_id, transcript_segments.seq,
       transcript_segments.speaker, transcript_segments.start_s, transcript_segments.end_s,
       transcript_segments.text, transcript_segments.created_at
FROM transcript_segments
JOIN meetings ON meetings.id = transcript_segments.meeting_id
WHERE transcript_segments.meeting_id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(after_seq)::integer IS NULL OR transcript_segments.seq > sqlc.narg(after_seq)::integer)
ORDER BY transcript_segments.seq
LIMIT sqlc.arg(page_size);

-- name: CountSegmentsForMeeting :one
SELECT COUNT(*) FROM transcript_segments
JOIN meetings ON meetings.id = transcript_segments.meeting_id
WHERE transcript_segments.meeting_id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListFolders :many
SELECT * FROM folders
WHERE workspace_id = sqlc.arg(workspace_id)
ORDER BY name, id;

-- name: UpdateFolderDetails :one
UPDATE folders SET
    name = COALESCE(sqlc.narg(name), name),
    parent_id = CASE
        WHEN sqlc.arg(clear_parent)::boolean THEN NULL
        ELSE COALESCE(sqlc.narg(parent_id), parent_id)
    END
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: CountChildFolders :one
SELECT COUNT(*) FROM folders
WHERE workspace_id = sqlc.arg(workspace_id) AND parent_id = sqlc.arg(parent_id);

-- name: ListShareLinksForMeeting :many
SELECT share_links.* FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE share_links.meeting_id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
ORDER BY share_links.created_at DESC, share_links.id DESC;

-- name: GetMeetingByShareTokenHash :one
SELECT sqlc.embed(meetings), sqlc.embed(share_links)
FROM share_links
JOIN meetings ON meetings.id = share_links.meeting_id
WHERE share_links.token_hash = sqlc.arg(token_hash);

-- name: DeleteShareLinksForMeeting :execrows
DELETE FROM share_links
USING meetings
WHERE share_links.meeting_id = meetings.id
  AND meetings.id = sqlc.arg(meeting_id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
