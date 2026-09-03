package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

type Store struct {
	pool    *pgxpool.Pool
	queries *sqlcgen.Queries
}

func NewStore(pool *pgxpool.Pool) (*Store, error) {
	if pool == nil {
		return nil, errors.New("auth: a database pool is required")
	}
	return &Store{pool: pool, queries: sqlcgen.New(pool)}, nil
}

func (s *Store) inTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (s *Store) TryCreateOTP(ctx context.Context, params TryCreateOTPParams) (OTPRecord, error) {
	var record OTPRecord
	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		if err := q.LockAuthOTPSendDestination(ctx, params.Destination); err != nil {
			return fmt.Errorf("lock destination: %w", err)
		}
		if params.RequestIP != "" {
			if err := q.LockAuthOTPSendIP(ctx, params.RequestIP); err != nil {
				return fmt.Errorf("lock address: %w", err)
			}
		}

		byDestination, err := q.CountAuthOTPsByDestination(ctx, sqlcgen.CountAuthOTPsByDestinationParams{
			Channel:     string(params.Channel),
			Destination: params.Destination,
			Since:       timestamp(params.DestinationSince),
		})
		if err != nil {
			return fmt.Errorf("count codes by destination: %w", err)
		}
		if byDestination >= params.DestinationLimit {
			return ErrDestinationRateLimited
		}

		if params.RequestIP != "" {
			byIP, err := q.CountAuthOTPsByIP(ctx, sqlcgen.CountAuthOTPsByIPParams{
				RequestIp: params.RequestIP,
				Since:     timestamp(params.IPSince),
			})
			if err != nil {
				return fmt.Errorf("count codes by address: %w", err)
			}
			if byIP >= params.IPLimit {
				return ErrIPRateLimited
			}
		}

		row, err := q.CreateAuthOTP(ctx, sqlcgen.CreateAuthOTPParams{
			Channel:     string(params.Channel),
			Destination: params.Destination,
			CodeHash:    params.CodeHash,
			ExpiresAt:   timestamp(params.ExpiresAt),
			RequestIp:   params.RequestIP,
			CreatedAt:   timestamp(params.CreatedAt),
		})
		if err != nil {
			return fmt.Errorf("create verification code: %w", err)
		}
		record = otpFromRow(row)
		return nil
	})
	if err != nil {
		return OTPRecord{}, err
	}
	return record, nil
}

func (s *Store) LatestOTP(ctx context.Context, channel Channel, destination string) (OTPRecord, error) {
	row, err := s.queries.GetLatestAuthOTP(ctx, sqlcgen.GetLatestAuthOTPParams{
		Channel:     string(channel),
		Destination: destination,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OTPRecord{}, ErrCodeNotFound
		}
		return OTPRecord{}, fmt.Errorf("read verification code: %w", err)
	}
	return otpFromRow(row), nil
}

func (s *Store) ClaimOTPAttempt(ctx context.Context, params ClaimOTPAttemptParams) (int32, error) {
	var attempts int32
	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		if err := q.LockAuthOTPVerifyDestination(ctx, params.Destination); err != nil {
			return fmt.Errorf("lock destination: %w", err)
		}
		if params.RequestIP != "" {
			if err := q.LockAuthOTPVerifyIP(ctx, params.RequestIP); err != nil {
				return fmt.Errorf("lock address: %w", err)
			}
		}

		byDestination, err := q.CountOTPVerifyAttemptsByDestination(ctx, sqlcgen.CountOTPVerifyAttemptsByDestinationParams{
			Destination: params.Destination,
			Since:       timestamp(params.DestinationSince),
		})
		if err != nil {
			return fmt.Errorf("count verify attempts by destination: %w", err)
		}
		if byDestination >= params.DestinationLimit {
			return ErrVerifyDestinationRateLimited
		}

		if params.RequestIP != "" {
			byIP, err := q.CountOTPVerifyAttemptsByIP(ctx, sqlcgen.CountOTPVerifyAttemptsByIPParams{
				Ip:    params.RequestIP,
				Since: timestamp(params.IPSince),
			})
			if err != nil {
				return fmt.Errorf("count verify attempts by address: %w", err)
			}
			if byIP >= params.IPLimit {
				return ErrVerifyIPRateLimited
			}
		}

		if err := q.CreateOTPVerifyAttempt(ctx, sqlcgen.CreateOTPVerifyAttemptParams{
			Destination: params.Destination,
			Ip:          params.RequestIP,
			CreatedAt:   timestamp(params.Now),
		}); err != nil {
			return fmt.Errorf("record verify attempt: %w", err)
		}

		claimed, err := q.ClaimAuthOTPAttempt(ctx, sqlcgen.ClaimAuthOTPAttemptParams{
			ID:          params.ID,
			MaxAttempts: params.MaxAttempts,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrTooManyAttempts
			}
			return fmt.Errorf("claim verification attempt: %w", err)
		}
		attempts = claimed
		return nil
	})
	if err != nil {
		return 0, err
	}
	return attempts, nil
}

func (s *Store) ConsumeOTP(ctx context.Context, id uuid.UUID, at time.Time) error {
	rows, err := s.queries.ConsumeAuthOTP(ctx, sqlcgen.ConsumeAuthOTPParams{
		ID:         id,
		ConsumedAt: timestamp(at),
	})
	if err != nil {
		return fmt.Errorf("consume verification code: %w", err)
	}
	if rows == 0 {
		return ErrCodeNotFound
	}
	return nil
}

func (s *Store) DeleteExpiredOTPs(ctx context.Context, before time.Time) (int64, error) {
	rows, err := s.queries.DeleteExpiredAuthOTPs(ctx, timestamp(before))
	if err != nil {
		return 0, fmt.Errorf("delete expired verification codes: %w", err)
	}
	return rows, nil
}

func (s *Store) CreateRefreshToken(ctx context.Context, params CreateRefreshParams) (RefreshRecord, error) {
	row, err := s.queries.CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		UserID:    params.UserID,
		FamilyID:  params.FamilyID,
		TokenHash: params.TokenHash,
		DeviceID:  params.DeviceID,
		ExpiresAt: timestamp(params.ExpiresAt),
		CreatedAt: timestamp(params.CreatedAt),
	})
	if err != nil {
		return RefreshRecord{}, fmt.Errorf("create refresh token: %w", err)
	}
	return refreshFromRow(row), nil
}

func (s *Store) GetRefreshTokenByHash(ctx context.Context, hash string) (RefreshRecord, error) {
	row, err := s.queries.GetRefreshTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RefreshRecord{}, ErrRefreshNotFound
		}
		return RefreshRecord{}, fmt.Errorf("read refresh token: %w", err)
	}
	return refreshFromRow(row), nil
}

func (s *Store) MarkRefreshTokenUsed(ctx context.Context, id uuid.UUID, at time.Time) (bool, error) {
	rows, err := s.queries.MarkRefreshTokenUsed(ctx, sqlcgen.MarkRefreshTokenUsedParams{
		UsedAt: timestamp(at),
		ID:     id,
	})
	if err != nil {
		return false, fmt.Errorf("mark refresh token used: %w", err)
	}
	return rows > 0, nil
}

func (s *Store) RevokeRefreshFamily(ctx context.Context, familyID uuid.UUID, at time.Time) (int64, error) {
	rows, err := s.queries.RevokeRefreshFamily(ctx, sqlcgen.RevokeRefreshFamilyParams{
		RevokedAt: timestamp(at),
		FamilyID:  familyID,
	})
	if err != nil {
		return 0, fmt.Errorf("revoke refresh token family: %w", err)
	}
	return rows, nil
}

func (s *Store) RevokeUserRefreshTokens(ctx context.Context, userID uuid.UUID, at time.Time) (int64, error) {
	rows, err := s.queries.RevokeUserRefreshTokens(ctx, sqlcgen.RevokeUserRefreshTokensParams{
		RevokedAt: timestamp(at),
		UserID:    userID,
	})
	if err != nil {
		return 0, fmt.Errorf("revoke refresh tokens: %w", err)
	}
	return rows, nil
}

func (s *Store) DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error) {
	rows, err := s.queries.DeleteExpiredRefreshTokens(ctx, timestamp(before))
	if err != nil {
		return 0, fmt.Errorf("delete expired refresh tokens: %w", err)
	}
	return rows, nil
}

func (s *Store) CreateOAuthState(ctx context.Context, params CreateOAuthStateParams) (OAuthStateRecord, error) {
	row, err := s.queries.CreateOAuthState(ctx, sqlcgen.CreateOAuthStateParams{
		Provider:     params.Provider,
		StateHash:    params.StateHash,
		NonceHash:    params.NonceHash,
		CodeVerifier: params.CodeVerifier,
		RedirectTo:   params.RedirectTo,
		ExpiresAt:    timestamp(params.ExpiresAt),
		CreatedAt:    timestamp(params.CreatedAt),
	})
	if err != nil {
		return OAuthStateRecord{}, fmt.Errorf("create oauth state: %w", err)
	}
	return oauthStateFromRow(row), nil
}

func (s *Store) ConsumeOAuthState(ctx context.Context, provider, stateHash string, at time.Time) (OAuthStateRecord, error) {
	row, err := s.queries.ConsumeOAuthState(ctx, sqlcgen.ConsumeOAuthStateParams{
		ConsumedAt: timestamp(at),
		StateHash:  stateHash,
		Provider:   provider,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OAuthStateRecord{}, ErrStateNotFound
		}
		return OAuthStateRecord{}, fmt.Errorf("consume oauth state: %w", err)
	}
	return oauthStateFromRow(row), nil
}

func (s *Store) DeleteExpiredOAuthStates(ctx context.Context, before time.Time) (int64, error) {
	rows, err := s.queries.DeleteExpiredOAuthStates(ctx, timestamp(before))
	if err != nil {
		return 0, fmt.Errorf("delete expired oauth states: %w", err)
	}
	return rows, nil
}

func otpFromRow(row sqlcgen.AuthOtp) OTPRecord {
	return OTPRecord{
		ID:          row.ID,
		Channel:     Channel(row.Channel),
		Destination: row.Destination,
		CodeHash:    row.CodeHash,
		ExpiresAt:   moment(row.ExpiresAt),
		Attempts:    row.Attempts,
		ConsumedAt:  optionalMoment(row.ConsumedAt),
		RequestIP:   row.RequestIp,
		CreatedAt:   moment(row.CreatedAt),
	}
}

func refreshFromRow(row sqlcgen.RefreshToken) RefreshRecord {
	return RefreshRecord{
		ID:         row.ID,
		UserID:     row.UserID,
		FamilyID:   row.FamilyID,
		TokenHash:  row.TokenHash,
		DeviceID:   row.DeviceID,
		ExpiresAt:  moment(row.ExpiresAt),
		RevokedAt:  optionalMoment(row.RevokedAt),
		LastUsedAt: optionalMoment(row.LastUsedAt),
		CreatedAt:  moment(row.CreatedAt),
	}
}

func oauthStateFromRow(row sqlcgen.OauthState) OAuthStateRecord {
	return OAuthStateRecord{
		ID:           row.ID,
		Provider:     row.Provider,
		StateHash:    row.StateHash,
		NonceHash:    row.NonceHash,
		CodeVerifier: row.CodeVerifier,
		RedirectTo:   row.RedirectTo,
		ExpiresAt:    moment(row.ExpiresAt),
		ConsumedAt:   optionalMoment(row.ConsumedAt),
		CreatedAt:    moment(row.CreatedAt),
	}
}

func timestamp(at time.Time) pgtype.Timestamptz {
	if at.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: at.UTC(), Valid: true}
}

func moment(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func optionalMoment(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time.UTC()
	return &at
}
