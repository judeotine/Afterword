-- name: CreateAuthOTP :one
INSERT INTO auth_otps (channel, destination, code_hash, expires_at, request_ip, created_at)
VALUES (
    sqlc.arg(channel),
    sqlc.arg(destination),
    sqlc.arg(code_hash),
    sqlc.arg(expires_at),
    sqlc.arg(request_ip),
    sqlc.arg(created_at)
)
RETURNING *;

-- name: LockAuthOTPDestination :exec
SELECT pg_advisory_xact_lock(hashtext(sqlc.arg(destination)::text));

-- name: TryCreateAuthOTP :one
INSERT INTO auth_otps (channel, destination, code_hash, expires_at, request_ip, created_at)
SELECT
    sqlc.arg(channel),
    sqlc.arg(destination),
    sqlc.arg(code_hash),
    sqlc.arg(expires_at),
    sqlc.arg(request_ip),
    sqlc.arg(created_at)
WHERE (
    SELECT count(*) FROM auth_otps AS by_destination
    WHERE by_destination.channel = sqlc.arg(channel)
      AND by_destination.destination = sqlc.arg(destination)
      AND by_destination.created_at >= sqlc.arg(destination_since)
) < sqlc.arg(destination_limit)::bigint
  AND (
    sqlc.arg(request_ip)::text = ''
    OR (
        SELECT count(*) FROM auth_otps AS by_ip
        WHERE by_ip.request_ip = sqlc.arg(request_ip)::text
          AND by_ip.request_ip <> ''
          AND by_ip.created_at >= sqlc.arg(ip_since)
    ) < sqlc.arg(ip_limit)::bigint
  )
RETURNING *;

-- name: GetLatestAuthOTP :one
SELECT * FROM auth_otps
WHERE channel = sqlc.arg(channel)
  AND destination = sqlc.arg(destination)
  AND consumed_at IS NULL
ORDER BY created_at DESC, id DESC
LIMIT 1;

-- name: CountAuthOTPsByDestination :one
SELECT count(*) FROM auth_otps
WHERE channel = sqlc.arg(channel)
  AND destination = sqlc.arg(destination)
  AND created_at >= sqlc.arg(since);

-- name: CountAuthOTPsByIP :one
SELECT count(*) FROM auth_otps
WHERE request_ip = sqlc.arg(request_ip)
  AND request_ip <> ''
  AND created_at >= sqlc.arg(since);

-- name: ClaimAuthOTPAttempt :one
UPDATE auth_otps SET attempts = auth_otps.attempts + 1
WHERE auth_otps.id = sqlc.arg(id)
  AND auth_otps.consumed_at IS NULL
  AND auth_otps.attempts < sqlc.arg(max_attempts)
  AND (
      SELECT COALESCE(sum(by_destination.attempts), 0) FROM auth_otps AS by_destination
      WHERE by_destination.channel = sqlc.arg(channel)
        AND by_destination.destination = sqlc.arg(destination)
        AND by_destination.created_at >= sqlc.arg(destination_since)
  ) < sqlc.arg(destination_limit)::bigint
  AND (
      sqlc.arg(request_ip)::text = ''
      OR (
          SELECT COALESCE(sum(by_ip.attempts), 0) FROM auth_otps AS by_ip
          WHERE by_ip.request_ip = sqlc.arg(request_ip)::text
            AND by_ip.request_ip <> ''
            AND by_ip.created_at >= sqlc.arg(ip_since)
      ) < sqlc.arg(ip_limit)::bigint
  )
RETURNING auth_otps.attempts;

-- name: ConsumeAuthOTP :execrows
UPDATE auth_otps SET consumed_at = sqlc.arg(consumed_at)
WHERE id = sqlc.arg(id) AND consumed_at IS NULL;

-- name: DeleteExpiredAuthOTPs :execrows
DELETE FROM auth_otps WHERE expires_at < sqlc.arg(before);
