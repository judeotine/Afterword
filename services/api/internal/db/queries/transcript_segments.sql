-- name: CreateTranscriptSegment :one
INSERT INTO transcript_segments (meeting_id, seq, speaker, start_s, end_s, text, embedding)
SELECT sqlc.arg(meeting_id), sqlc.arg(seq), sqlc.narg(speaker), sqlc.arg(start_s),
       sqlc.arg(end_s), sqlc.arg(text), sqlc.narg(embedding)::vector
FROM meetings
WHERE meetings.id = sqlc.arg(meeting_id) AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING id, meeting_id, seq, speaker, start_s, end_s, text, COALESCE(embedding::text, '')::text AS embedding, created_at;

-- name: GetTranscriptSegment :one
SELECT transcript_segments.id, transcript_segments.meeting_id, transcript_segments.seq,
       transcript_segments.speaker, transcript_segments.start_s, transcript_segments.end_s,
       transcript_segments.text, COALESCE(transcript_segments.embedding::text, '')::text AS embedding, transcript_segments.created_at
FROM transcript_segments
JOIN meetings ON meetings.id = transcript_segments.meeting_id
WHERE transcript_segments.id = sqlc.arg(id) AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListTranscriptSegmentsByWorkspace :many
SELECT transcript_segments.id, transcript_segments.meeting_id, transcript_segments.seq,
       transcript_segments.speaker, transcript_segments.start_s, transcript_segments.end_s,
       transcript_segments.text, COALESCE(transcript_segments.embedding::text, '')::text AS embedding, transcript_segments.created_at
FROM transcript_segments
JOIN meetings ON meetings.id = transcript_segments.meeting_id
WHERE meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(meeting_id)::uuid IS NULL OR transcript_segments.meeting_id = sqlc.narg(meeting_id)::uuid)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (transcript_segments.created_at, transcript_segments.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY transcript_segments.created_at DESC, transcript_segments.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateTranscriptSegment :one
UPDATE transcript_segments SET
    speaker = COALESCE(sqlc.narg(speaker), speaker),
    start_s = COALESCE(sqlc.narg(start_s), start_s),
    end_s = COALESCE(sqlc.narg(end_s), end_s),
    text = COALESCE(sqlc.narg(text), text),
    embedding = COALESCE(sqlc.narg(embedding)::vector, embedding)
FROM meetings
WHERE transcript_segments.meeting_id = meetings.id
  AND transcript_segments.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING transcript_segments.id, transcript_segments.meeting_id, transcript_segments.seq,
       transcript_segments.speaker, transcript_segments.start_s, transcript_segments.end_s,
       transcript_segments.text, COALESCE(transcript_segments.embedding::text, '')::text AS embedding, transcript_segments.created_at;

-- name: DeleteTranscriptSegment :execrows
DELETE FROM transcript_segments
USING meetings
WHERE transcript_segments.meeting_id = meetings.id
  AND transcript_segments.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
