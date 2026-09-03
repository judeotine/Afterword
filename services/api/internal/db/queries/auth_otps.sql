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

-- name: LockAuthOTPSendDestination :exec
SELECT pg_advisory_xact_lock(hashtext('otp-send-destination:' || sqlc.arg(destination)::text));

-- name: LockAuthOTPSendIP :exec
SELECT pg_advisory_xact_lock(hashtext('otp-send-ip:' || sqlc.arg(ip)::text));

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

-- name: LockAuthOTPVerifyDestination :exec
SELECT pg_advisory_xact_lock(hashtext('otp-verify-destination:' || sqlc.arg(destination)::text));

-- name: LockAuthOTPVerifyIP :exec
SELECT pg_advisory_xact_lock(hashtext('otp-verify-ip:' || sqlc.arg(ip)::text));

-- name: ClaimAuthOTPAttempt :one
UPDATE auth_otps SET attempts = attempts + 1
WHERE id = sqlc.arg(id)
  AND consumed_at IS NULL
  AND attempts < sqlc.arg(max_attempts)
RETURNING attempts;

-- name: ConsumeAuthOTP :execrows
UPDATE auth_otps SET consumed_at = sqlc.arg(consumed_at)
WHERE id = sqlc.arg(id) AND consumed_at IS NULL;

-- name: DeleteExpiredAuthOTPs :execrows
DELETE FROM auth_otps WHERE expires_at < sqlc.arg(before);
