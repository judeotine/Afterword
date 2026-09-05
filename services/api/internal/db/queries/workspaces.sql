-- name: CreateWorkspace :one
INSERT INTO workspaces (name, slug, bot_name, retention_days)
VALUES (sqlc.arg(name), sqlc.arg(slug), sqlc.arg(bot_name), sqlc.arg(retention_days))
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces WHERE id = sqlc.arg(id);

-- name: GetWorkspaceBySlug :one
SELECT * FROM workspaces WHERE slug = sqlc.arg(slug);

-- name: ListWorkspacesByUser :many
SELECT workspaces.* FROM workspaces
JOIN memberships ON memberships.workspace_id = workspaces.id
WHERE memberships.user_id = sqlc.arg(user_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (workspaces.created_at, workspaces.id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY workspaces.created_at DESC, workspaces.id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateWorkspace :one
UPDATE workspaces SET
    name = COALESCE(sqlc.narg(name), name),
    slug = COALESCE(sqlc.narg(slug), slug),
    bot_name = COALESCE(sqlc.narg(bot_name), bot_name),
    retention_days = COALESCE(sqlc.narg(retention_days), retention_days)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces WHERE id = sqlc.arg(id);

-- name: ListWorkspacesWithRoleByUser :many
SELECT workspaces.*, memberships.role AS membership_role
FROM workspaces
JOIN memberships ON memberships.workspace_id = workspaces.id
WHERE memberships.user_id = sqlc.arg(user_id)
ORDER BY memberships.created_at ASC, workspaces.id ASC;

-- name: WorkspaceSlugExists :one
SELECT EXISTS (SELECT 1 FROM workspaces WHERE slug = sqlc.arg(slug));

-- name: GetDefaultWorkspaceForUser :one
SELECT workspaces.*, memberships.role AS membership_role
FROM workspaces
JOIN memberships ON memberships.workspace_id = workspaces.id
WHERE memberships.user_id = sqlc.arg(user_id)
ORDER BY memberships.created_at ASC, workspaces.id ASC
LIMIT 1;
