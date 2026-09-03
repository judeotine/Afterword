-- name: CreateMembership :one
INSERT INTO memberships (workspace_id, user_id, role)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role))
RETURNING *;

-- name: GetMembership :one
SELECT * FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: ListMembershipsByWorkspace :many
SELECT * FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, user_id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, user_id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateMembership :one
UPDATE memberships SET role = sqlc.arg(role)
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id)
RETURNING *;

-- name: DeleteMembership :execrows
DELETE FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id) AND user_id = sqlc.arg(user_id);

-- name: CountWorkspaceOwners :one
SELECT count(*) FROM memberships
WHERE workspace_id = sqlc.arg(workspace_id) AND role = 'owner';

-- name: UpsertMembership :one
INSERT INTO memberships (workspace_id, user_id, role)
VALUES (sqlc.arg(workspace_id), sqlc.arg(user_id), sqlc.arg(role))
ON CONFLICT (workspace_id, user_id) DO UPDATE SET role = excluded.role
RETURNING *;

-- name: ListWorkspaceMembersWithUsers :many
SELECT
    memberships.workspace_id,
    memberships.user_id,
    memberships.role,
    memberships.created_at,
    users.email,
    users.phone,
    users.name
FROM memberships
JOIN users ON users.id = memberships.user_id
WHERE memberships.workspace_id = sqlc.arg(workspace_id)
ORDER BY memberships.created_at ASC, memberships.user_id ASC;
