package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

type memoryStateStore struct {
	mu      sync.Mutex
	records map[string]auth.OAuthStateRecord
}

func newMemoryStateStore() *memoryStateStore {
	return &memoryStateStore{records: map[string]auth.OAuthStateRecord{}}
}

func (s *memoryStateStore) CreateOAuthState(_ context.Context, params auth.CreateOAuthStateParams) (auth.OAuthStateRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record := auth.OAuthStateRecord{
		ID:           uuid.New(),
		Provider:     params.Provider,
		StateHash:    params.StateHash,
		CodeVerifier: params.CodeVerifier,
		RedirectTo:   params.RedirectTo,
		ExpiresAt:    params.ExpiresAt,
		CreatedAt:    params.CreatedAt,
	}
	s.records[params.StateHash] = record
	return record, nil
}

func (s *memoryStateStore) ConsumeOAuthState(_ context.Context, provider, stateHash string, at time.Time) (auth.OAuthStateRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.records[stateHash]
	if !ok || record.Provider != provider || record.ConsumedAt != nil {
		return auth.OAuthStateRecord{}, auth.ErrStateNotFound
	}
	consumed := at
	record.ConsumedAt = &consumed
	s.records[stateHash] = record
	return record, nil
}

type fakeGoogle struct {
	server        *httptest.Server
	profile       map[string]any
	lastVerifier  string
	lastCode      string
	tokenStatus   int
	profileStatus int
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	t.Helper()
	google := &fakeGoogle{
		profile: map[string]any{
			"sub":            "google-subject-1",
			"email":          "Person@Example.com",
			"email_verified": true,
			"name":           "A Person",
		},
		tokenStatus:   http.StatusOK,
		profileStatus: http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		google.lastVerifier = r.Form.Get("code_verifier")
		google.lastCode = r.Form.Get("code")
		if google.tokenStatus != http.StatusOK {
			w.WriteHeader(google.tokenStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "google-access-token",
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer google-access-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if google.profileStatus != http.StatusOK {
			w.WriteHeader(google.profileStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(google.profile)
	})
	google.server = httptest.NewServer(mux)
	t.Cleanup(google.server.Close)
	return google
}

type googleFixture struct {
	authenticator *auth.GoogleAuthenticator
	google        *fakeGoogle
	store         *memoryStateStore
	now           time.Time
}

func newGoogleFixture(t *testing.T) *googleFixture {
	t.Helper()
	fixture := &googleFixture{
		google: newFakeGoogle(t),
		store:  newMemoryStateStore(),
		now:    time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC),
	}
	authenticator, err := auth.NewGoogleAuthenticator(auth.GoogleOptions{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://api.example.com/v1/auth/google/callback",
		Store:        fixture.store,
		AuthURL:      fixture.google.server.URL + "/auth",
		TokenURL:     fixture.google.server.URL + "/token",
		UserInfoURL:  fixture.google.server.URL + "/userinfo",
		HTTPClient:   fixture.google.server.Client(),
		Clock:        func() time.Time { return fixture.now },
	})
	if err != nil {
		t.Fatalf("new google authenticator: %v", err)
	}
	fixture.authenticator = authenticator
	return fixture
}

func TestGoogleStartBuildsAPKCEAuthorizationURL(t *testing.T) {
	fixture := newGoogleFixture(t)

	started, err := fixture.authenticator.Start(context.Background(), "/library")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	parsed, err := url.Parse(started.AuthorizationURL)
	if err != nil {
		t.Fatalf("parse authorization url: %v", err)
	}
	query := parsed.Query()
	if query.Get("state") != started.State {
		t.Fatalf("state %q does not match %q", query.Get("state"), started.State)
	}
	if query.Get("code_challenge_method") != "S256" {
		t.Fatalf("challenge method %q", query.Get("code_challenge_method"))
	}
	if query.Get("code_challenge") == "" {
		t.Fatal("no code challenge")
	}
	if query.Get("client_id") != "client-id" {
		t.Fatalf("client id %q", query.Get("client_id"))
	}
	if query.Get("redirect_uri") != "https://api.example.com/v1/auth/google/callback" {
		t.Fatalf("redirect uri %q", query.Get("redirect_uri"))
	}
	if !started.ExpiresAt.Equal(fixture.now.Add(auth.DefaultOAuthStateTTL)) {
		t.Fatalf("expires at %v", started.ExpiresAt)
	}
	for _, record := range fixture.store.records {
		if record.StateHash == started.State {
			t.Fatal("the raw state was stored")
		}
		if record.CodeVerifier == "" {
			t.Fatal("no verifier was stored")
		}
	}
}

func TestGoogleCompleteReturnsTheVerifiedProfile(t *testing.T) {
	fixture := newGoogleFixture(t)

	started, err := fixture.authenticator.Start(context.Background(), "/library")
	if err != nil {
		t.Fatalf("start: %v", err)
	}

	completed, err := fixture.authenticator.Complete(context.Background(), started.State, "authorization-code")
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if completed.Profile.Email != "person@example.com" {
		t.Fatalf("email %q, want the normalised address", completed.Profile.Email)
	}
	if completed.Profile.Subject != "google-subject-1" || completed.Profile.Name != "A Person" {
		t.Fatalf("profile %+v", completed.Profile)
	}
	if completed.RedirectTo != "/library" {
		t.Fatalf("redirect %q", completed.RedirectTo)
	}
	if fixture.google.lastVerifier == "" {
		t.Fatal("no code verifier was sent to the token endpoint")
	}
	if fixture.google.lastCode != "authorization-code" {
		t.Fatalf("code %q", fixture.google.lastCode)
	}
}

func TestGoogleCompleteAcceptsAStateOnlyOnce(t *testing.T) {
	fixture := newGoogleFixture(t)
	started, err := fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); !errors.Is(err, auth.ErrStateNotFound) {
		t.Fatalf("replay: got %v, want ErrStateNotFound", err)
	}
}

func TestGoogleCompleteRejectsAnUnknownOrEmptyState(t *testing.T) {
	fixture := newGoogleFixture(t)
	for _, state := range []string{"", "  ", "unknown-state"} {
		if _, err := fixture.authenticator.Complete(context.Background(), state, "code"); !errors.Is(err, auth.ErrStateNotFound) {
			t.Fatalf("state %q: got %v, want ErrStateNotFound", state, err)
		}
	}
	if _, err := fixture.authenticator.Complete(context.Background(), "state", ""); !errors.Is(err, auth.ErrStateNotFound) {
		t.Fatalf("empty code: got %v", err)
	}
}

func TestGoogleCompleteRejectsAnExpiredState(t *testing.T) {
	fixture := newGoogleFixture(t)
	started, err := fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	fixture.now = fixture.now.Add(auth.DefaultOAuthStateTTL)
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); !errors.Is(err, auth.ErrStateExpired) {
		t.Fatalf("got %v, want ErrStateExpired", err)
	}
}

func TestGoogleCompleteRejectsAnUnverifiedEmail(t *testing.T) {
	fixture := newGoogleFixture(t)
	fixture.google.profile["email_verified"] = false

	started, err := fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); !errors.Is(err, auth.ErrEmailNotVerified) {
		t.Fatalf("got %v, want ErrEmailNotVerified", err)
	}
}

func TestGoogleCompleteAcceptsAStringEmailVerifiedFlag(t *testing.T) {
	fixture := newGoogleFixture(t)
	fixture.google.profile["email_verified"] = "true"

	started, err := fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); err != nil {
		t.Fatalf("complete: %v", err)
	}
}

func TestGoogleCompleteReportsProviderFailures(t *testing.T) {
	fixture := newGoogleFixture(t)
	fixture.google.tokenStatus = http.StatusBadRequest

	started, err := fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); err == nil {
		t.Fatal("expected the exchange failure to surface")
	}

	fixture.google.tokenStatus = http.StatusOK
	fixture.google.profileStatus = http.StatusInternalServerError
	started, err = fixture.authenticator.Start(context.Background(), "")
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if _, err := fixture.authenticator.Complete(context.Background(), started.State, "code"); err == nil {
		t.Fatal("expected the profile failure to surface")
	}
}

func TestNewGoogleAuthenticatorRequiresCredentials(t *testing.T) {
	store := newMemoryStateStore()
	cases := []auth.GoogleOptions{
		{ClientSecret: "s", RedirectURL: "https://api.example.com/cb", Store: store},
		{ClientID: "c", RedirectURL: "https://api.example.com/cb", Store: store},
		{ClientID: "c", ClientSecret: "s", Store: store},
	}
	for _, options := range cases {
		if _, err := auth.NewGoogleAuthenticator(options); !errors.Is(err, auth.ErrNotConfigured) {
			t.Fatalf("options %+v: got %v, want ErrNotConfigured", options, err)
		}
	}
	if _, err := auth.NewGoogleAuthenticator(auth.GoogleOptions{
		ClientID: "c", ClientSecret: "s", RedirectURL: "https://api.example.com/cb",
	}); err == nil {
		t.Fatal("expected an error without a state store")
	}
}

func TestSafeRedirectPath(t *testing.T) {
	cases := map[string]string{
		"":                              "",
		"/library":                      "/library",
		"/library?folder=1":             "/library?folder=1",
		"//evil.example.com":            "",
		"https://evil.example.com/x":    "",
		"library":                       "",
		"http://evil.example.com":       "",
		"/\\evil.example.com":           "/%5Cevil.example.com",
		"   /meetings/42   ":            "/meetings/42",
		"javascript:alert(document.co)": "",
	}
	for input, want := range cases {
		if got := auth.SafeRedirectPath(input); got != want {
			t.Fatalf("SafeRedirectPath(%q) = %q, want %q", input, got, want)
		}
	}
}
