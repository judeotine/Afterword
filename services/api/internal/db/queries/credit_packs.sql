-- name: CreateCreditPack :one
INSERT INTO credit_packs (name, minutes, price_minor, currency, active)
VALUES (sqlc.arg(name), sqlc.arg(minutes), sqlc.arg(price_minor), sqlc.arg(currency), sqlc.arg(active))
RETURNING *;

-- name: GetCreditPack :one
SELECT * FROM credit_packs WHERE id = sqlc.arg(id);

-- name: ListCreditPacks :many
SELECT * FROM credit_packs
WHERE (sqlc.narg(active)::boolean IS NULL OR active = sqlc.narg(active)::boolean)
  AND (
      sqlc.narg(cursor_created_at)::timestamptz IS NULL
      OR (created_at, id) < (sqlc.narg(cursor_created_at)::timestamptz, sqlc.narg(cursor_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(page_size);

-- name: UpdateCreditPack :one
UPDATE credit_packs SET
    name = COALESCE(sqlc.narg(name), name),
    minutes = COALESCE(sqlc.narg(minutes), minutes),
    price_minor = COALESCE(sqlc.narg(price_minor), price_minor),
    currency = COALESCE(sqlc.narg(currency), currency),
    active = COALESCE(sqlc.narg(active), active)
WHERE id = sqlc.arg(id)
RETURNING *;

-- name: DeleteCreditPack :execrows
DELETE FROM credit_packs WHERE id = sqlc.arg(id);
