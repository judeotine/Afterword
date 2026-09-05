-- name: CreateDevice :one
INSERT INTO devices (user_id, name, platform)
VALUES (sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(platform))
RETURNING *;

-- name: GetDevice :one
SELECT devices.* FROM devices
JOIN memberships ON memberships.user_id = devices.user_id
WHERE devices.id = sqlc.arg(id) AND memberships.workspace_id = sqlc.arg(workspace_id);

-- name: ListDevicesByWorkspace :many
SELECT DISTINCT ON (devices.created_at, devices.id) devices.*
FROM devices
JOIN memberships ON memberships.user_id = devices.user_id
WHERE memberships.workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (devices.created_at, devices.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY devices.created_at DESC, devices.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateDevice :one
UPDATE devices SET
    name = COALESCE(sqlc.narg(name), name),
    last_sync_at = COALESCE(sqlc.narg(last_sync_at), last_sync_at)
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteDevice :execrows
DELETE FROM devices
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id);
