package auth

import "errors"

var (
	ErrWeakSecret         = errors.New("auth: signing secret is too short")
	ErrInvalidRole        = errors.New("auth: role is not recognised")
	ErrInvalidSubject     = errors.New("auth: subject is required")
	ErrInvalidToken       = errors.New("auth: token is not valid")
	ErrExpiredToken       = errors.New("auth: token has expired")
	ErrNotConfigured      = errors.New("auth: provider is not configured")
	ErrInvalidChannel     = errors.New("auth: channel is not recognised")
	ErrInvalidDestination = errors.New("auth: destination is not valid")
	ErrCodeNotFound       = errors.New("auth: no active code for that destination")
	ErrCodeExpired        = errors.New("auth: code has expired")
	ErrCodeMismatch       = errors.New("auth: code does not match")
	ErrTooManyAttempts    = errors.New("auth: too many verification attempts")
	ErrRateLimited        = errors.New("auth: too many requests")
	ErrRefreshNotFound    = errors.New("auth: refresh token is not valid")
	ErrRefreshExpired     = errors.New("auth: refresh token has expired")
	ErrRefreshRevoked     = errors.New("auth: refresh token has been revoked")
	ErrRefreshReused      = errors.New("auth: refresh token was already used")
	ErrStateNotFound      = errors.New("auth: oauth state is not valid")
	ErrStateExpired       = errors.New("auth: oauth state has expired")
	ErrEmailNotVerified   = errors.New("auth: google account email is not verified")
)
