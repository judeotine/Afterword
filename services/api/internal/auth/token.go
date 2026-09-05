package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	DefaultAccessTokenTTL = 15 * time.Minute
	MinimumSecretLength   = 32
	tokenIDBytes          = 16
)

type Claims struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
	Role        Role
	TokenID     string
	IssuedAt    time.Time
	ExpiresAt   time.Time
}

type IssuedToken struct {
	Token     string
	TokenID   string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
	issuer string
	clock  func() time.Time
}

type TokenIssuerOption func(*TokenIssuer)

func WithTokenTTL(ttl time.Duration) TokenIssuerOption {
	return func(i *TokenIssuer) {
		if ttl > 0 {
			i.ttl = ttl
		}
	}
}

func WithTokenIssuerName(name string) TokenIssuerOption {
	return func(i *TokenIssuer) { i.issuer = name }
}

func WithTokenClock(clock func() time.Time) TokenIssuerOption {
	return func(i *TokenIssuer) {
		if clock != nil {
			i.clock = clock
		}
	}
}

func NewTokenIssuer(secret []byte, opts ...TokenIssuerOption) (*TokenIssuer, error) {
	if len(secret) < MinimumSecretLength {
		return nil, ErrWeakSecret
	}
	issuer := &TokenIssuer{
		secret: append([]byte(nil), secret...),
		ttl:    DefaultAccessTokenTTL,
		issuer: "afterword",
		clock:  time.Now,
	}
	for _, opt := range opts {
		opt(issuer)
	}
	return issuer, nil
}

func (i *TokenIssuer) TTL() time.Duration {
	return i.ttl
}

func (i *TokenIssuer) Issue(userID, workspaceID uuid.UUID, role Role) (IssuedToken, error) {
	if userID == uuid.Nil {
		return IssuedToken{}, ErrInvalidSubject
	}
	if !role.Valid() {
		return IssuedToken{}, ErrInvalidRole
	}

	tokenID, err := newTokenID()
	if err != nil {
		return IssuedToken{}, err
	}

	issuedAt := i.clock().UTC().Truncate(time.Second)
	expiresAt := issuedAt.Add(i.ttl)

	workspace := ""
	if workspaceID != uuid.Nil {
		workspace = workspaceID.String()
	}

	claims := jwt.MapClaims{
		"sub":  userID.String(),
		"ws":   workspace,
		"role": string(role),
		"jti":  tokenID,
		"iat":  issuedAt.Unix(),
		"exp":  expiresAt.Unix(),
		"iss":  i.issuer,
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(i.secret)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("sign access token: %w", err)
	}

	return IssuedToken{Token: signed, TokenID: tokenID, IssuedAt: issuedAt, ExpiresAt: expiresAt}, nil
}

func (i *TokenIssuer) Verify(token string) (Claims, error) {
	parsed, err := jwt.Parse(token,
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(i.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return i.clock() }),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return Claims{}, ErrExpiredToken
		}
		return Claims{}, ErrInvalidToken
	}

	body, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrInvalidToken
	}

	userID, err := uuidClaim(body, "sub")
	if err != nil || userID == uuid.Nil {
		return Claims{}, ErrInvalidToken
	}

	workspaceID := uuid.Nil
	if raw, _ := body["ws"].(string); raw != "" {
		workspaceID, err = uuid.Parse(raw)
		if err != nil {
			return Claims{}, ErrInvalidToken
		}
	}

	roleValue, _ := body["role"].(string)
	role, err := ParseRole(roleValue)
	if err != nil {
		return Claims{}, ErrInvalidToken
	}

	tokenID, _ := body["jti"].(string)
	if tokenID == "" {
		return Claims{}, ErrInvalidToken
	}

	expiresAt, err := parsed.Claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return Claims{}, ErrInvalidToken
	}
	issuedAt, err := parsed.Claims.GetIssuedAt()
	if err != nil || issuedAt == nil {
		return Claims{}, ErrInvalidToken
	}

	return Claims{
		UserID:      userID,
		WorkspaceID: workspaceID,
		Role:        role,
		TokenID:     tokenID,
		IssuedAt:    issuedAt.UTC(),
		ExpiresAt:   expiresAt.UTC(),
	}, nil
}

func uuidClaim(claims jwt.MapClaims, name string) (uuid.UUID, error) {
	raw, ok := claims[name].(string)
	if !ok || raw == "" {
		return uuid.Nil, ErrInvalidToken
	}
	parsed, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, ErrInvalidToken
	}
	return parsed, nil
}

func newTokenID() (string, error) {
	buf := make([]byte, tokenIDBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
