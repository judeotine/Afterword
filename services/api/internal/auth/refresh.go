package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultRefreshTokenTTL = 30 * 24 * time.Hour
	RefreshTokenBytes      = 32
	maxDeviceIDLength      = 128
)

type RefreshRecord struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	FamilyID   uuid.UUID
	TokenHash  string
	DeviceID   string
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	LastUsedAt *time.Time
	CreatedAt  time.Time
}

type CreateRefreshParams struct {
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	TokenHash string
	DeviceID  string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type RefreshStore interface {
	CreateRefreshToken(ctx context.Context, params CreateRefreshParams) (RefreshRecord, error)
	GetRefreshTokenByHash(ctx context.Context, hash string) (RefreshRecord, error)
	MarkRefreshTokenUsed(ctx context.Context, id uuid.UUID, at time.Time) (bool, error)
	RevokeRefreshFamily(ctx context.Context, familyID uuid.UUID, at time.Time) (int64, error)
}

type RefreshToken struct {
	Token     string
	ID        uuid.UUID
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	DeviceID  string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

type RefreshManagerOptions struct {
	Store RefreshStore
	TTL   time.Duration
	Clock func() time.Time
}

type RefreshManager struct {
	store RefreshStore
	ttl   time.Duration
	clock func() time.Time
}

func NewRefreshManager(options RefreshManagerOptions) (*RefreshManager, error) {
	if options.Store == nil {
		return nil, errors.New("auth: a refresh token store is required")
	}
	manager := &RefreshManager{
		store: options.Store,
		ttl:   positiveDuration(options.TTL, DefaultRefreshTokenTTL),
		clock: options.Clock,
	}
	if manager.clock == nil {
		manager.clock = time.Now
	}
	return manager, nil
}

func (m *RefreshManager) TTL() time.Duration {
	return m.ttl
}

func (m *RefreshManager) Issue(ctx context.Context, userID uuid.UUID, deviceID string) (RefreshToken, error) {
	if userID == uuid.Nil {
		return RefreshToken{}, ErrInvalidSubject
	}
	return m.mint(ctx, userID, uuid.New(), deviceID)
}

func (m *RefreshManager) Rotate(ctx context.Context, presented, deviceID string) (RefreshToken, error) {
	record, err := m.lookup(ctx, presented)
	if err != nil {
		return RefreshToken{}, err
	}

	now := m.clock().UTC()

	if record.RevokedAt != nil {
		if record.LastUsedAt == nil {
			return RefreshToken{}, ErrRefreshRevoked
		}
		if _, err := m.store.RevokeRefreshFamily(ctx, record.FamilyID, now); err != nil {
			return RefreshToken{}, err
		}
		return RefreshToken{}, ErrRefreshReused
	}

	if !now.Before(record.ExpiresAt) {
		return RefreshToken{}, ErrRefreshExpired
	}

	claimed, err := m.store.MarkRefreshTokenUsed(ctx, record.ID, now)
	if err != nil {
		return RefreshToken{}, err
	}
	if !claimed {
		if _, err := m.store.RevokeRefreshFamily(ctx, record.FamilyID, now); err != nil {
			return RefreshToken{}, err
		}
		return RefreshToken{}, ErrRefreshReused
	}

	presentedDevice := normalizeDeviceID(deviceID)
	if presentedDevice != "" && presentedDevice != record.DeviceID {
		if _, err := m.store.RevokeRefreshFamily(ctx, record.FamilyID, now); err != nil {
			return RefreshToken{}, err
		}
		return RefreshToken{}, ErrRefreshReused
	}

	return m.mint(ctx, record.UserID, record.FamilyID, record.DeviceID)
}

func (m *RefreshManager) Revoke(ctx context.Context, presented string) error {
	record, err := m.lookup(ctx, presented)
	if err != nil {
		return err
	}
	if _, err := m.store.RevokeRefreshFamily(ctx, record.FamilyID, m.clock().UTC()); err != nil {
		return err
	}
	return nil
}

func (m *RefreshManager) lookup(ctx context.Context, presented string) (RefreshRecord, error) {
	token := strings.TrimSpace(presented)
	if token == "" {
		return RefreshRecord{}, ErrRefreshNotFound
	}
	record, err := m.store.GetRefreshTokenByHash(ctx, HashRefreshToken(token))
	if err != nil {
		return RefreshRecord{}, err
	}
	return record, nil
}

func (m *RefreshManager) mint(ctx context.Context, userID, familyID uuid.UUID, deviceID string) (RefreshToken, error) {
	token, err := newRefreshSecret()
	if err != nil {
		return RefreshToken{}, err
	}

	now := m.clock().UTC()
	expiresAt := now.Add(m.ttl)

	record, err := m.store.CreateRefreshToken(ctx, CreateRefreshParams{
		UserID:    userID,
		FamilyID:  familyID,
		TokenHash: HashRefreshToken(token),
		DeviceID:  normalizeDeviceID(deviceID),
		ExpiresAt: expiresAt,
		CreatedAt: now,
	})
	if err != nil {
		return RefreshToken{}, err
	}

	return RefreshToken{
		Token:     token,
		ID:        record.ID,
		UserID:    record.UserID,
		FamilyID:  record.FamilyID,
		DeviceID:  record.DeviceID,
		IssuedAt:  now,
		ExpiresAt: record.ExpiresAt,
	}, nil
}

func HashToken(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func HashRefreshToken(token string) string {
	return HashToken(token)
}

func NewOpaqueToken(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("auth: token size must be positive")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func newRefreshSecret() (string, error) {
	return NewOpaqueToken(RefreshTokenBytes)
}

func normalizeDeviceID(deviceID string) string {
	trimmed := strings.TrimSpace(deviceID)
	if len(trimmed) > maxDeviceIDLength {
		return trimmed[:maxDeviceIDLength]
	}
	return trimmed
}
