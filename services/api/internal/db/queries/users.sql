-- name: CreateUser :one
INSERT INTO users (email, phone, name)
VALUES (sqlc.narg(email), sqlc.narg(phone), sqlc.arg(name))
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = sqlc.arg(id);

-- name: GetUserInWorkspace :one
SELECT users.* FROM users
JOIN memberships ON memberships.user_id = users.id
WHERE users.id = sqlc.arg(id) AND memberships.workspace_id = sqlc.arg(workspace_id);

-- name: ListUsersByWorkspace :many
SELECT users.* FROM users
JOIN memberships ON memberships.user_id = users.id
WHERE memberships.workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (users.created_at, users.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY users.created_at DESC, users.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateUser :one
UPDATE users SET
    email = COALESCE(sqlc.narg(email), email),
    phone = COALESCE(sqlc.narg(phone), phone),
    name = COALESCE(sqlc.narg(name), name)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteUser :execrows
DELETE FROM users WHERE id = sqlc.arg(id);
