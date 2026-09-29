package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const createUser = `-- name: CreateUser :one
INSERT INTO users (email, phone, name)
VALUES ($1, $2, $3)
RETURNING id, email, phone, name, created_at
`

type CreateUserParams struct {
	Email *string `json:"email"`
	Phone *string `json:"phone"`
	Name  string  `json:"name"`
}

func (q *Queries) CreateUser(ctx context.Context, arg CreateUserParams) (User, error) {
	row := q.db.QueryRow(ctx, createUser, arg.Email, arg.Phone, arg.Name)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}

const deleteUser = `-- name: DeleteUser :execrows
DELETE FROM users WHERE id = $1
`

func (q *Queries) DeleteUser(ctx context.Context, id uuid.UUID) (int64, error) {
	result, err := q.db.Exec(ctx, deleteUser, id)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getUser = `-- name: GetUser :one
SELECT id, email, phone, name, created_at FROM users WHERE id = $1
`

func (q *Queries) GetUser(ctx context.Context, id uuid.UUID) (User, error) {
	row := q.db.QueryRow(ctx, getUser, id)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}

const getUserByEmail = `-- name: GetUserByEmail :one
SELECT id, email, phone, name, created_at FROM users WHERE email IS NOT NULL AND lower(email) = lower($1)
`

func (q *Queries) GetUserByEmail(ctx context.Context, email string) (User, error) {
	row := q.db.QueryRow(ctx, getUserByEmail, email)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}

const getUserByPhone = `-- name: GetUserByPhone :one
SELECT id, email, phone, name, created_at FROM users WHERE phone IS NOT NULL AND phone = $1::text
`

func (q *Queries) GetUserByPhone(ctx context.Context, phone string) (User, error) {
	row := q.db.QueryRow(ctx, getUserByPhone, phone)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}

const getUserInWorkspace = `-- name: GetUserInWorkspace :one
SELECT users.id, users.email, users.phone, users.name, users.created_at FROM users
JOIN memberships ON memberships.user_id = users.id
WHERE users.id = $1 AND memberships.workspace_id = $2
`

type GetUserInWorkspaceParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetUserInWorkspace(ctx context.Context, arg GetUserInWorkspaceParams) (User, error) {
	row := q.db.QueryRow(ctx, getUserInWorkspace, arg.ID, arg.WorkspaceID)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}

const listUsersByWorkspace = `-- name: ListUsersByWorkspace :many
SELECT users.id, users.email, users.phone, users.name, users.created_at FROM users
JOIN memberships ON memberships.user_id = users.id
WHERE memberships.workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (users.created_at, users.id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY users.created_at DESC, users.id DESC
LIMIT $4
`

type ListUsersByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListUsersByWorkspace(ctx context.Context, arg ListUsersByWorkspaceParams) ([]User, error) {
	rows, err := q.db.Query(ctx, listUsersByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []User{}
	for rows.Next() {
		var i User
		if err := rows.Scan(
			&i.ID,
			&i.Email,
			&i.Phone,
			&i.Name,
			&i.CreatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateUser = `-- name: UpdateUser :one
UPDATE users SET
    email = COALESCE($1, email),
    phone = COALESCE($2, phone),
    name = COALESCE($3, name)
WHERE id = $4
RETURNING id, email, phone, name, created_at
`

type UpdateUserParams struct {
	Email *string   `json:"email"`
	Phone *string   `json:"phone"`
	Name  *string   `json:"name"`
	ID    uuid.UUID `json:"id"`
}

func (q *Queries) UpdateUser(ctx context.Context, arg UpdateUserParams) (User, error) {
	row := q.db.QueryRow(ctx, updateUser,
		arg.Email,
		arg.Phone,
		arg.Name,
		arg.ID,
	)
	var i User
	err := row.Scan(
		&i.ID,
		&i.Email,
		&i.Phone,
		&i.Name,
		&i.CreatedAt,
	)
	return i, err
}
