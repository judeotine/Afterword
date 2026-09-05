-- name: CreateSummary :one
INSERT INTO summaries (meeting_id, template_id, language, markdown, model, action_items)
SELECT sqlc.arg(meeting_id), sqlc.narg(template_id), sqlc.arg(language), sqlc.arg(markdown),
       sqlc.arg(model), sqlc.arg(action_items)
FROM meetings
WHERE meetings.id = sqlc.arg(meeting_id) AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: GetSummary :one
SELECT summaries.* FROM summaries
JOIN meetings ON meetings.id = summaries.meeting_id
WHERE summaries.id = sqlc.arg(id) AND meetings.workspace_id = sqlc.arg(workspace_id);

-- name: ListSummariesByWorkspace :many
SELECT summaries.* FROM summaries
JOIN meetings ON meetings.id = summaries.meeting_id
WHERE meetings.workspace_id = sqlc.arg(workspace_id)
  AND (sqlc.narg(meeting_id)::uuid IS NULL OR summaries.meeting_id = sqlc.narg(meeting_id)::uuid)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (summaries.created_at, summaries.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY summaries.created_at DESC, summaries.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateSummary :one
UPDATE summaries SET
    template_id = COALESCE(sqlc.narg(template_id), template_id),
    language = COALESCE(sqlc.narg(language), language),
    markdown = COALESCE(sqlc.narg(markdown), markdown),
    model = COALESCE(sqlc.narg(model), model),
    action_items = COALESCE(sqlc.narg(action_items), action_items)
FROM meetings
WHERE summaries.meeting_id = meetings.id
  AND summaries.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id)
RETURNING summaries.*;

-- name: DeleteSummary :execrows
DELETE FROM summaries
USING meetings
WHERE summaries.meeting_id = meetings.id
  AND summaries.id = sqlc.arg(id)
  AND meetings.workspace_id = sqlc.arg(workspace_id);
