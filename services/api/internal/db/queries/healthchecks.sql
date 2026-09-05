-- name: GetHealthcheck :one
SELECT id, at FROM healthchecks WHERE id = 1;

-- name: TouchHealthcheck :one
UPDATE healthchecks SET at = now() WHERE id = 1 RETURNING id, at;
