-- name: CountOTPVerifyAttemptsByDestination :one
SELECT count(*) FROM auth_otp_verify_attempts
WHERE destination = sqlc.arg(destination)
  AND created_at >= sqlc.arg(since);

-- name: CountOTPVerifyAttemptsByIP :one
SELECT count(*) FROM auth_otp_verify_attempts
WHERE ip = sqlc.arg(ip)
  AND ip <> ''
  AND created_at >= sqlc.arg(since);

-- name: CreateOTPVerifyAttempt :exec
INSERT INTO auth_otp_verify_attempts (destination, ip, created_at)
VALUES (sqlc.arg(destination), sqlc.arg(ip), sqlc.arg(created_at));

-- name: DeleteExpiredOTPVerifyAttempts :execrows
DELETE FROM auth_otp_verify_attempts WHERE created_at < sqlc.arg(before);
