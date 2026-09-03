//go:build integration

package auth_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
)

func newStore(t *testing.T) (*auth.Store, *pgxpool.Pool) {
	t.Helper()
	pool := dbtest.New(t)
	store, err := auth.NewStore(pool)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	return store, pool
}

func newTestUser(t *testing.T, pool *pgxpool.Pool, email string) uuid.UUID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`, email, "Test Person",
	).Scan(&id); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return id
}

func TestStoreOTPLifecycle(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	record, err := store.TryCreateOTP(ctx, auth.TryCreateOTPParams{
		Channel:          auth.ChannelEmail,
		Destination:      "person@example.com",
		CodeHash:         "hash-1",
		ExpiresAt:        now.Add(10 * time.Minute),
		RequestIP:        "203.0.113.7",
		CreatedAt:        now,
		DestinationSince: now.Add(-auth.DefaultDestinationWindow),
		DestinationLimit: auth.DefaultSendsPerDestination,
		IPSince:          now.Add(-auth.DefaultIPWindow),
		IPLimit:          auth.DefaultSendsPerIP,
	})
	if err != nil {
		t.Fatalf("create otp: %v", err)
	}
	if record.ID == uuid.Nil || record.Attempts != 0 || record.ConsumedAt != nil {
		t.Fatalf("record %+v", record)
	}

	latest, err := store.LatestOTP(ctx, auth.ChannelEmail, "person@example.com")
	if err != nil {
		t.Fatalf("latest otp: %v", err)
	}
	if latest.ID != record.ID {
		t.Fatalf("latest %v, want %v", latest.ID, record.ID)
	}

	attempts, err := store.ClaimOTPAttempt(ctx, auth.ClaimOTPAttemptParams{
		ID:               record.ID,
		Destination:      "person@example.com",
		MaxAttempts:      auth.DefaultOTPMaxAttempts,
		Now:              now,
		DestinationSince: now.Add(-auth.DefaultVerifyDestinationWindow),
		DestinationLimit: auth.DefaultVerifiesPerDestination,
		IPSince:          now.Add(-auth.DefaultVerifyIPWindow),
		IPLimit:          auth.DefaultVerifiesPerIP,
	})
	if err != nil || attempts != 1 {
		t.Fatalf("claim attempt: %d, %v", attempts, err)
	}

	if err := store.ConsumeOTP(ctx, record.ID, now.Add(time.Minute)); err != nil {
		t.Fatalf("consume: %v", err)
	}
	if err := store.ConsumeOTP(ctx, record.ID, now.Add(time.Minute)); !errors.Is(err, auth.ErrCodeNotFound) {
		t.Fatalf("second consume: got %v, want ErrCodeNotFound", err)
	}
	if _, err := store.LatestOTP(ctx, auth.ChannelEmail, "person@example.com"); !errors.Is(err, auth.ErrCodeNotFound) {
		t.Fatalf("latest after consume: got %v, want ErrCodeNotFound", err)
	}
}

func TestStoreDeleteExpiredOTPs(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for i := 0; i < 3; i++ {
		if _, err := store.TryCreateOTP(ctx, auth.TryCreateOTPParams{
			Channel:          auth.ChannelEmail,
			Destination:      "person@example.com",
			CodeHash:         "hash",
			ExpiresAt:        now.Add(10 * time.Minute),
			RequestIP:        "203.0.113.7",
			CreatedAt:        now.Add(-time.Duration(i) * time.Minute),
			DestinationSince: now.Add(-time.Hour),
			DestinationLimit: 100,
			IPSince:          now.Add(-time.Hour),
			IPLimit:          100,
		}); err != nil {
			t.Fatalf("create otp %d: %v", i, err)
		}
	}
	if _, err := store.TryCreateOTP(ctx, auth.TryCreateOTPParams{
		Channel:          auth.ChannelEmail,
		Destination:      "person@example.com",
		CodeHash:         "old",
		ExpiresAt:        now.Add(-90 * time.Minute),
		RequestIP:        "203.0.113.7",
		CreatedAt:        now.Add(-2 * time.Hour),
		DestinationSince: now.Add(-3 * time.Hour),
		DestinationLimit: 100,
		IPSince:          now.Add(-3 * time.Hour),
		IPLimit:          100,
	}); err != nil {
		t.Fatalf("create old otp: %v", err)
	}

	removed, err := store.DeleteExpiredOTPs(ctx, now)
	if err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d rows, want 1", removed)
	}
}

func TestOTPServiceAgainstPostgres(t *testing.T) {
	store, _ := newStore(t)
	sender := &recordingSender{}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	ctx := context.Background()

	if _, err := service.Send(ctx, auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		RequestIP:   "203.0.113.7",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	emails := sender.sentEmails()
	if len(emails) != 1 {
		t.Fatalf("sent %d emails", len(emails))
	}
	code := codeFromBody(t, emails[0].Body)

	if _, err := service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        "000000",
	}); err == nil {
		t.Fatal("a wrong code was accepted")
	}

	verified, err := service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.Destination != "person@example.com" {
		t.Fatalf("verified %+v", verified)
	}

	if _, err := service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
		Code:        code,
	}); !errors.Is(err, auth.ErrCodeNotFound) {
		t.Fatalf("replay: got %v, want ErrCodeNotFound", err)
	}

	for i := 0; i < auth.DefaultSendsPerDestination-1; i++ {
		if _, err := service.Send(ctx, auth.SendCodeRequest{
			Channel:     auth.ChannelEmail,
			Destination: "person@example.com",
		}); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if _, err := service.Send(ctx, auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "person@example.com",
	}); !errors.Is(err, auth.ErrRateLimited) {
		t.Fatalf("rate limit: got %v, want ErrRateLimited", err)
	}
}

func TestRefreshManagerAgainstPostgres(t *testing.T) {
	store, pool := newStore(t)
	userID := newTestUser(t, pool, "person@example.com")

	manager, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: store})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	ctx := context.Background()

	first, err := manager.Issue(ctx, userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	second, err := manager.Rotate(ctx, first.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if second.FamilyID != first.FamilyID || second.DeviceID != "desktop-1" {
		t.Fatalf("rotated %+v", second)
	}

	if _, err := manager.Rotate(ctx, first.Token, ""); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("replay: got %v, want ErrRefreshReused", err)
	}
	if _, err := manager.Rotate(ctx, second.Token, ""); !errors.Is(err, auth.ErrRefreshRevoked) {
		t.Fatalf("live token after replay: got %v, want ErrRefreshRevoked", err)
	}
	if _, err := manager.Rotate(ctx, "not-a-token", ""); !errors.Is(err, auth.ErrRefreshNotFound) {
		t.Fatalf("unknown token: got %v", err)
	}
}

func TestRefreshRotationIsAtomicUnderConcurrency(t *testing.T) {
	store, pool := newStore(t)
	userID := newTestUser(t, pool, "person@example.com")

	manager, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: store})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	ctx := context.Background()

	issued, err := manager.Issue(ctx, userID, "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	const racers = 8
	results := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func() {
			_, err := manager.Rotate(ctx, issued.Token, "")
			results <- err
		}()
	}

	winners := 0
	for i := 0; i < racers; i++ {
		switch err := <-results; {
		case err == nil:
			winners++
		case errors.Is(err, auth.ErrRefreshReused), errors.Is(err, auth.ErrRefreshRevoked):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d rotations succeeded, want exactly 1", winners)
	}
}

func TestOTPVerifyAttemptClaimIsAtomicUnderConcurrency(t *testing.T) {
	store, _ := newStore(t)
	sender := &recordingSender{}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	ctx := context.Background()

	if _, err := service.Send(ctx, auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: "racer@example.com",
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	const racers = 50
	results := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func() {
			_, err := service.Verify(ctx, auth.VerifyCodeRequest{
				Channel:     auth.ChannelEmail,
				Destination: "racer@example.com",
				Code:        "000000",
			})
			results <- err
		}()
	}

	var mismatches, lockedOut int
	for i := 0; i < racers; i++ {
		switch err := <-results; {
		case errors.Is(err, auth.ErrCodeMismatch):
			mismatches++
		case errors.Is(err, auth.ErrTooManyAttempts):
			lockedOut++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if mismatches != auth.DefaultOTPMaxAttempts {
		t.Fatalf("%d guesses were compared, want exactly %d", mismatches, auth.DefaultOTPMaxAttempts)
	}
	if lockedOut != racers-auth.DefaultOTPMaxAttempts {
		t.Fatalf("%d guesses were locked out, want %d", lockedOut, racers-auth.DefaultOTPMaxAttempts)
	}

	record, err := store.LatestOTP(ctx, auth.ChannelEmail, "racer@example.com")
	if err != nil {
		t.Fatalf("latest otp: %v", err)
	}
	if record.Attempts != auth.DefaultOTPMaxAttempts {
		t.Fatalf("stored attempts = %d, want exactly %d: the claim raced past the cap", record.Attempts, auth.DefaultOTPMaxAttempts)
	}
}

func TestOTPSendIsAtomicUnderConcurrency(t *testing.T) {
	store, pool := newStore(t)
	sender := &recordingSender{}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	ctx := context.Background()

	const racers = 20
	results := make(chan error, racers)
	for i := 0; i < racers; i++ {
		go func() {
			_, err := service.Send(ctx, auth.SendCodeRequest{
				Channel:     auth.ChannelEmail,
				Destination: "burst@example.com",
			})
			results <- err
		}()
	}

	var sent, limited int
	for i := 0; i < racers; i++ {
		switch err := <-results; {
		case err == nil:
			sent++
		case errors.Is(err, auth.ErrRateLimited):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if sent != auth.DefaultSendsPerDestination {
		t.Fatalf("%d sends succeeded, want exactly %d", sent, auth.DefaultSendsPerDestination)
	}
	if limited != racers-auth.DefaultSendsPerDestination {
		t.Fatalf("%d sends were rate limited, want %d", limited, racers-auth.DefaultSendsPerDestination)
	}

	rows := countAuthOTPRows(t, pool, "burst@example.com")
	if rows != int64(auth.DefaultSendsPerDestination) {
		t.Fatalf("%d rows were inserted, want exactly %d: the rate limit raced past the cap", rows, auth.DefaultSendsPerDestination)
	}
}

func countAuthOTPRows(t *testing.T, pool *pgxpool.Pool, destination string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var count int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM auth_otps WHERE destination = $1`, destination,
	).Scan(&count); err != nil {
		t.Fatalf("count auth_otps rows: %v", err)
	}
	return count
}

func countAuthOTPVerifyAttemptRows(t *testing.T, pool *pgxpool.Pool, ip string) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	var count int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM auth_otp_verify_attempts WHERE ip = $1`, ip,
	).Scan(&count); err != nil {
		t.Fatalf("count auth_otp_verify_attempts rows: %v", err)
	}
	return count
}

func TestOTPVerifyDestinationRateLimitFires(t *testing.T) {
	store, _ := newStore(t)
	sender := &recordingSender{}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	ctx := context.Background()
	destination := "destination-limit@example.com"

	if _, err := service.Send(ctx, auth.SendCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: destination,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}

	for i := int64(0); i < auth.DefaultVerifiesPerDestination; i++ {
		_, err := service.Verify(ctx, auth.VerifyCodeRequest{
			Channel:     auth.ChannelEmail,
			Destination: destination,
			Code:        "000000",
		})
		if errors.Is(err, auth.ErrVerifyDestinationRateLimited) {
			t.Fatalf("attempt %d: the destination limit fired early", i+1)
		}
		if err == nil || (!errors.Is(err, auth.ErrCodeMismatch) && !errors.Is(err, auth.ErrTooManyAttempts)) {
			t.Fatalf("attempt %d: unexpected error %v", i+1, err)
		}
	}

	_, err = service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: destination,
		Code:        "000000",
	})
	if !errors.Is(err, auth.ErrVerifyDestinationRateLimited) {
		t.Fatalf("11th attempt: got %v, want ErrVerifyDestinationRateLimited", err)
	}
}

func TestOTPVerifyIPRateLimitFiresAcrossDestinations(t *testing.T) {
	store, pool := newStore(t)
	sender := &recordingSender{}
	service, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}
	ctx := context.Background()

	const (
		destinations   = 10
		perDestination = 6
		clientIP       = "203.0.113.44"
	)
	if destinations*perDestination < int(auth.DefaultVerifiesPerIP) {
		t.Fatalf("test setup does not reach the ip limit: %d < %d", destinations*perDestination, auth.DefaultVerifiesPerIP)
	}
	if perDestination >= int(auth.DefaultVerifiesPerDestination) {
		t.Fatalf("test setup would trip the destination limit first: %d >= %d", perDestination, auth.DefaultVerifiesPerDestination)
	}

	firstDestination := "ip-limit-0@example.com"
	for d := 0; d < destinations; d++ {
		destination := fmt.Sprintf("ip-limit-%d@example.com", d)
		if _, err := service.Send(ctx, auth.SendCodeRequest{
			Channel:     auth.ChannelEmail,
			Destination: destination,
		}); err != nil {
			t.Fatalf("send %d: %v", d, err)
		}
		for i := 0; i < perDestination; i++ {
			_, err := service.Verify(ctx, auth.VerifyCodeRequest{
				Channel:     auth.ChannelEmail,
				Destination: destination,
				Code:        "000000",
				RequestIP:   clientIP,
			})
			if errors.Is(err, auth.ErrVerifyIPRateLimited) {
				t.Fatalf("destination %d attempt %d: the ip limit fired early", d, i+1)
			}
		}
	}

	if rows := countAuthOTPVerifyAttemptRows(t, pool, clientIP); rows != int64(destinations*perDestination) {
		t.Fatalf("recorded %d verify attempts for that address, want %d", rows, destinations*perDestination)
	}

	_, err = service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: firstDestination,
		Code:        "000000",
		RequestIP:   clientIP,
	})
	if !errors.Is(err, auth.ErrVerifyIPRateLimited) {
		t.Fatalf("61st attempt: got %v, want ErrVerifyIPRateLimited", err)
	}

	elsewhere, err := service.Verify(ctx, auth.VerifyCodeRequest{
		Channel:     auth.ChannelEmail,
		Destination: firstDestination,
		Code:        "000000",
		RequestIP:   "203.0.113.45",
	})
	if errors.Is(err, auth.ErrVerifyIPRateLimited) {
		t.Fatalf("a different address was limited: %v", elsewhere)
	}
}

func TestStoreOAuthStateIsSingleUse(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if _, err := store.CreateOAuthState(ctx, auth.CreateOAuthStateParams{
		Provider:     auth.GoogleProvider,
		StateHash:    "state-hash",
		CodeVerifier: "verifier",
		RedirectTo:   "/library",
		ExpiresAt:    now.Add(10 * time.Minute),
		CreatedAt:    now,
	}); err != nil {
		t.Fatalf("create state: %v", err)
	}

	record, err := store.ConsumeOAuthState(ctx, auth.GoogleProvider, "state-hash", now)
	if err != nil {
		t.Fatalf("consume state: %v", err)
	}
	if record.CodeVerifier != "verifier" || record.RedirectTo != "/library" {
		t.Fatalf("record %+v", record)
	}

	if _, err := store.ConsumeOAuthState(ctx, auth.GoogleProvider, "state-hash", now); !errors.Is(err, auth.ErrStateNotFound) {
		t.Fatalf("second consume: got %v, want ErrStateNotFound", err)
	}
	if _, err := store.ConsumeOAuthState(ctx, auth.GoogleProvider, "missing", now); !errors.Is(err, auth.ErrStateNotFound) {
		t.Fatalf("unknown state: got %v", err)
	}
}
