-- name: CreateCalendarConnection :one
INSERT INTO calendar_connections (user_id, provider, refresh_token_enc, auto_join_rule)
VALUES (sqlc.arg(user_id), sqlc.arg(provider), sqlc.arg(refresh_token_enc), sqlc.arg(auto_join_rule))
RETURNING *;

-- name: GetCalendarConnection :one
SELECT calendar_connections.* FROM calendar_connections
JOIN memberships ON memberships.user_id = calendar_connections.user_id
WHERE calendar_connections.id = sqlc.arg(id) AND memberships.workspace_id = sqlc.arg(workspace_id);

-- name: ListCalendarConnectionsByWorkspace :many
SELECT DISTINCT ON (calendar_connections.created_at, calendar_connections.id) calendar_connections.*
FROM calendar_connections
JOIN memberships ON memberships.user_id = calendar_connections.user_id
WHERE memberships.workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (calendar_connections.created_at, calendar_connections.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY calendar_connections.created_at DESC, calendar_connections.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateCalendarConnection :one
UPDATE calendar_connections SET
    refresh_token_enc = COALESCE(sqlc.narg(refresh_token_enc), refresh_token_enc),
    auto_join_rule = COALESCE(sqlc.narg(auto_join_rule), auto_join_rule)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteCalendarConnection :execrows
DELETE FROM calendar_connections
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);
