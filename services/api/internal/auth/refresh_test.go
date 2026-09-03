package auth_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

type memoryRefreshStore struct {
	mu      sync.Mutex
	records []auth.RefreshRecord
}

func newMemoryRefreshStore() *memoryRefreshStore {
	return &memoryRefreshStore{}
}

func (s *memoryRefreshStore) CreateRefreshToken(_ context.Context, params auth.CreateRefreshParams) (auth.RefreshRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := auth.RefreshRecord{
		ID:        uuid.New(),
		UserID:    params.UserID,
		FamilyID:  params.FamilyID,
		TokenHash: params.TokenHash,
		DeviceID:  params.DeviceID,
		ExpiresAt: params.ExpiresAt,
		CreatedAt: params.CreatedAt,
	}
	s.records = append(s.records, record)
	return record, nil
}

func (s *memoryRefreshStore) GetRefreshTokenByHash(_ context.Context, hash string) (auth.RefreshRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, record := range s.records {
		if record.TokenHash == hash {
			return record, nil
		}
	}
	return auth.RefreshRecord{}, auth.ErrRefreshNotFound
}

func (s *memoryRefreshStore) MarkRefreshTokenUsed(_ context.Context, id uuid.UUID, at time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.records {
		if s.records[i].ID != id {
			continue
		}
		if s.records[i].RevokedAt != nil {
			return false, nil
		}
		used := at
		s.records[i].RevokedAt = &used
		s.records[i].LastUsedAt = &used
		return true, nil
	}
	return false, auth.ErrRefreshNotFound
}

func (s *memoryRefreshStore) RevokeRefreshFamily(_ context.Context, familyID uuid.UUID, at time.Time) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var revoked int64
	for i := range s.records {
		if s.records[i].FamilyID == familyID && s.records[i].RevokedAt == nil {
			stamp := at
			s.records[i].RevokedAt = &stamp
			revoked++
		}
	}
	return revoked, nil
}

func (s *memoryRefreshStore) all() []auth.RefreshRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]auth.RefreshRecord(nil), s.records...)
}

type refreshFixture struct {
	manager *auth.RefreshManager
	store   *memoryRefreshStore
	now     time.Time
}

func newRefreshFixture(t *testing.T) *refreshFixture {
	t.Helper()
	fixture := &refreshFixture{
		store: newMemoryRefreshStore(),
		now:   time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC),
	}
	manager, err := auth.NewRefreshManager(auth.RefreshManagerOptions{
		Store: fixture.store,
		Clock: func() time.Time { return fixture.now },
	})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	fixture.manager = manager
	return fixture
}

func TestRefreshIssueReturnsAnOpaqueTokenAndStoresOnlyItsHash(t *testing.T) {
	fixture := newRefreshFixture(t)
	userID := uuid.New()

	issued, err := fixture.manager.Issue(context.Background(), userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(issued.Token)
	if err != nil {
		t.Fatalf("token is not raw base64url: %v", err)
	}
	if len(raw) != auth.RefreshTokenBytes {
		t.Fatalf("token carries %d bytes, want %d", len(raw), auth.RefreshTokenBytes)
	}
	if !issued.ExpiresAt.Equal(fixture.now.Add(auth.DefaultRefreshTokenTTL)) {
		t.Fatalf("expires at %v", issued.ExpiresAt)
	}

	records := fixture.store.all()
	if len(records) != 1 {
		t.Fatalf("stored %d records, want 1", len(records))
	}
	if strings.Contains(records[0].TokenHash, issued.Token) {
		t.Fatal("the stored hash contains the token")
	}
	digest := sha256.Sum256([]byte(issued.Token))
	if records[0].TokenHash != hex.EncodeToString(digest[:]) {
		t.Fatal("the stored hash is not the sha256 of the token")
	}
	if records[0].UserID != userID || records[0].DeviceID != "desktop-1" {
		t.Fatalf("stored %+v", records[0])
	}
	if records[0].FamilyID == uuid.Nil {
		t.Fatal("no family was assigned")
	}
}

func TestRefreshIssueGeneratesDistinctTokens(t *testing.T) {
	fixture := newRefreshFixture(t)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "")
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		if seen[issued.Token] {
			t.Fatal("duplicate refresh token")
		}
		seen[issued.Token] = true
	}
}

func TestRefreshRotationInvalidatesThePresentedToken(t *testing.T) {
	fixture := newRefreshFixture(t)
	userID := uuid.New()

	first, err := fixture.manager.Issue(context.Background(), userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	fixture.now = fixture.now.Add(time.Hour)
	rotated, err := fixture.manager.Rotate(context.Background(), first.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.Token == first.Token {
		t.Fatal("rotation returned the same token")
	}
	if rotated.UserID != userID {
		t.Fatalf("user %v, want %v", rotated.UserID, userID)
	}
	if rotated.DeviceID != "desktop-1" {
		t.Fatalf("device %q was not carried over", rotated.DeviceID)
	}
	if rotated.FamilyID != first.FamilyID {
		t.Fatalf("family changed from %v to %v", first.FamilyID, rotated.FamilyID)
	}
	if !rotated.ExpiresAt.Equal(fixture.now.Add(auth.DefaultRefreshTokenTTL)) {
		t.Fatalf("expires at %v", rotated.ExpiresAt)
	}

	records := fixture.store.all()
	if records[0].RevokedAt == nil {
		t.Fatal("the presented token was not revoked")
	}
	if records[0].LastUsedAt == nil || !records[0].LastUsedAt.Equal(fixture.now) {
		t.Fatalf("last used at %v", records[0].LastUsedAt)
	}

	if _, err := fixture.manager.Rotate(context.Background(), rotated.Token, ""); err != nil {
		t.Fatalf("the new token should rotate: %v", err)
	}
}

func TestRefreshReuseRevokesTheWholeFamily(t *testing.T) {
	fixture := newRefreshFixture(t)
	userID := uuid.New()

	first, err := fixture.manager.Issue(context.Background(), userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	second, err := fixture.manager.Rotate(context.Background(), first.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	third, err := fixture.manager.Rotate(context.Background(), second.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if _, err := fixture.manager.Rotate(context.Background(), first.Token, ""); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("replay: got %v, want ErrRefreshReused", err)
	}

	if _, err := fixture.manager.Rotate(context.Background(), third.Token, ""); !errors.Is(err, auth.ErrRefreshRevoked) {
		t.Fatalf("live token after a replay: got %v, want ErrRefreshRevoked", err)
	}

	for _, record := range fixture.store.all() {
		if record.RevokedAt == nil {
			t.Fatalf("record %v survived the family revocation", record.ID)
		}
	}
}

func TestRefreshReuseLeavesOtherFamiliesAlone(t *testing.T) {
	fixture := newRefreshFixture(t)
	userID := uuid.New()

	desktop, err := fixture.manager.Issue(context.Background(), userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	phone, err := fixture.manager.Issue(context.Background(), userID, "phone-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	rotatedDesktop, err := fixture.manager.Rotate(context.Background(), desktop.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if _, err := fixture.manager.Rotate(context.Background(), desktop.Token, ""); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("replay: got %v, want ErrRefreshReused", err)
	}
	if _, err := fixture.manager.Rotate(context.Background(), rotatedDesktop.Token, ""); !errors.Is(err, auth.ErrRefreshRevoked) {
		t.Fatalf("desktop family should be dead: %v", err)
	}

	if _, err := fixture.manager.Rotate(context.Background(), phone.Token, ""); err != nil {
		t.Fatalf("the phone family should still work: %v", err)
	}
}

func TestRefreshRotateRejectsAnExpiredToken(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	fixture.now = fixture.now.Add(auth.DefaultRefreshTokenTTL)
	if _, err := fixture.manager.Rotate(context.Background(), issued.Token, ""); !errors.Is(err, auth.ErrRefreshExpired) {
		t.Fatalf("got %v, want ErrRefreshExpired", err)
	}
}

func TestRefreshRotateAcceptsATokenJustBeforeExpiry(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	fixture.now = fixture.now.Add(auth.DefaultRefreshTokenTTL - time.Second)
	if _, err := fixture.manager.Rotate(context.Background(), issued.Token, ""); err != nil {
		t.Fatalf("rotate: %v", err)
	}
}

func TestRefreshRotateRejectsUnknownTokens(t *testing.T) {
	fixture := newRefreshFixture(t)
	for _, token := range []string{"", "   ", "nonsense", base64.RawURLEncoding.EncodeToString(make([]byte, 32))} {
		if _, err := fixture.manager.Rotate(context.Background(), token, ""); !errors.Is(err, auth.ErrRefreshNotFound) {
			t.Fatalf("token %q: got %v, want ErrRefreshNotFound", token, err)
		}
	}
}

func TestRefreshRevokeEndsTheFamily(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	rotated, err := fixture.manager.Rotate(context.Background(), issued.Token, "")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}

	if err := fixture.manager.Revoke(context.Background(), rotated.Token); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := fixture.manager.Rotate(context.Background(), rotated.Token, ""); !errors.Is(err, auth.ErrRefreshRevoked) {
		t.Fatalf("got %v, want ErrRefreshRevoked", err)
	}
}

func TestRefreshRevokeReportsUnknownTokens(t *testing.T) {
	fixture := newRefreshFixture(t)
	if err := fixture.manager.Revoke(context.Background(), "nonsense"); !errors.Is(err, auth.ErrRefreshNotFound) {
		t.Fatalf("got %v, want ErrRefreshNotFound", err)
	}
}

func TestRefreshConcurrentRotationYieldsOneWinner(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	const racers = 8
	var wg sync.WaitGroup
	results := make([]error, racers)
	wg.Add(racers)
	for i := 0; i < racers; i++ {
		go func(index int) {
			defer wg.Done()
			_, err := fixture.manager.Rotate(context.Background(), issued.Token, "")
			results[index] = err
		}(i)
	}
	wg.Wait()

	winners := 0
	for _, err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, auth.ErrRefreshReused):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if winners != 1 {
		t.Fatalf("%d rotations succeeded, want exactly 1", winners)
	}
}

func TestRefreshRotateRejectsAMismatchedDeviceAndRevokesTheFamily(t *testing.T) {
	fixture := newRefreshFixture(t)
	userID := uuid.New()

	issued, err := fixture.manager.Issue(context.Background(), userID, "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	if _, err := fixture.manager.Rotate(context.Background(), issued.Token, "someone-elses-device"); !errors.Is(err, auth.ErrRefreshReused) {
		t.Fatalf("got %v, want ErrRefreshReused", err)
	}

	for _, record := range fixture.store.all() {
		if record.RevokedAt == nil {
			t.Fatalf("record %v survived the device mismatch", record.ID)
		}
	}
}

func TestRefreshRotateAcceptsAMatchingDevice(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := fixture.manager.Rotate(context.Background(), issued.Token, "desktop-1"); err != nil {
		t.Fatalf("rotate with the matching device: %v", err)
	}
}

func TestRefreshRotateIgnoresAnUnassertedDevice(t *testing.T) {
	fixture := newRefreshFixture(t)
	issued, err := fixture.manager.Issue(context.Background(), uuid.New(), "desktop-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := fixture.manager.Rotate(context.Background(), issued.Token, ""); err != nil {
		t.Fatalf("rotate without asserting a device: %v", err)
	}
}

func TestNewRefreshManagerRequiresAStore(t *testing.T) {
	if _, err := auth.NewRefreshManager(auth.RefreshManagerOptions{}); err == nil {
		t.Fatal("expected an error when no store is given")
	}
}
