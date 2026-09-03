package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
)

const (
	GoogleProvider          = "google"
	DefaultOAuthStateTTL    = 10 * time.Minute
	googleAuthURL           = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL          = "https://oauth2.googleapis.com/token"
	googleUserInfoURL       = "https://openidconnect.googleapis.com/v1/userinfo"
	oauthStateBytes         = 32
	maxUserInfoResponseSize = 1 << 16
)

type OAuthStateRecord struct {
	ID           uuid.UUID
	Provider     string
	StateHash    string
	CodeVerifier string
	RedirectTo   string
	ExpiresAt    time.Time
	ConsumedAt   *time.Time
	CreatedAt    time.Time
}

type CreateOAuthStateParams struct {
	Provider     string
	StateHash    string
	CodeVerifier string
	RedirectTo   string
	ExpiresAt    time.Time
	CreatedAt    time.Time
}

type OAuthStateStore interface {
	CreateOAuthState(ctx context.Context, params CreateOAuthStateParams) (OAuthStateRecord, error)
	ConsumeOAuthState(ctx context.Context, provider, stateHash string, at time.Time) (OAuthStateRecord, error)
}

type GoogleProfile struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type StartedAuthorization struct {
	AuthorizationURL string
	State            string
	ExpiresAt        time.Time
}

type CompletedAuthorization struct {
	Profile    GoogleProfile
	RedirectTo string
}

type GoogleOptions struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Store        OAuthStateStore
	AuthURL      string
	TokenURL     string
	UserInfoURL  string
	HTTPClient   *http.Client
	StateTTL     time.Duration
	Clock        func() time.Time
}

type GoogleAuthenticator struct {
	config      *oauth2.Config
	store       OAuthStateStore
	userInfoURL string
	httpClient  *http.Client
	stateTTL    time.Duration
	clock       func() time.Time
}

func NewGoogleAuthenticator(options GoogleOptions) (*GoogleAuthenticator, error) {
	if strings.TrimSpace(options.ClientID) == "" || strings.TrimSpace(options.ClientSecret) == "" {
		return nil, fmt.Errorf("%w: GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET are required", ErrNotConfigured)
	}
	if strings.TrimSpace(options.RedirectURL) == "" {
		return nil, fmt.Errorf("%w: a google redirect url is required", ErrNotConfigured)
	}
	if options.Store == nil {
		return nil, errors.New("auth: an oauth state store is required")
	}

	authenticator := &GoogleAuthenticator{
		config: &oauth2.Config{
			ClientID:     options.ClientID,
			ClientSecret: options.ClientSecret,
			RedirectURL:  options.RedirectURL,
			Scopes:       []string{"openid", "email", "profile"},
			Endpoint: oauth2.Endpoint{
				AuthURL:  firstNonEmpty(options.AuthURL, googleAuthURL),
				TokenURL: firstNonEmpty(options.TokenURL, googleTokenURL),
			},
		},
		store:       options.Store,
		userInfoURL: firstNonEmpty(options.UserInfoURL, googleUserInfoURL),
		httpClient:  options.HTTPClient,
		stateTTL:    positiveDuration(options.StateTTL, DefaultOAuthStateTTL),
		clock:       options.Clock,
	}
	if authenticator.httpClient == nil {
		authenticator.httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	if authenticator.clock == nil {
		authenticator.clock = time.Now
	}
	return authenticator, nil
}

func (g *GoogleAuthenticator) Start(ctx context.Context, redirectTo string) (StartedAuthorization, error) {
	state, err := NewOpaqueToken(oauthStateBytes)
	if err != nil {
		return StartedAuthorization{}, err
	}
	verifier := oauth2.GenerateVerifier()

	now := g.clock().UTC()
	expiresAt := now.Add(g.stateTTL)

	if _, err := g.store.CreateOAuthState(ctx, CreateOAuthStateParams{
		Provider:     GoogleProvider,
		StateHash:    hashState(state),
		CodeVerifier: verifier,
		RedirectTo:   SafeRedirectPath(redirectTo),
		ExpiresAt:    expiresAt,
		CreatedAt:    now,
	}); err != nil {
		return StartedAuthorization{}, err
	}

	authorizationURL := g.config.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("prompt", "select_account"),
		oauth2.S256ChallengeOption(verifier),
	)

	return StartedAuthorization{AuthorizationURL: authorizationURL, State: state, ExpiresAt: expiresAt}, nil
}

func (g *GoogleAuthenticator) Complete(ctx context.Context, state, code string) (CompletedAuthorization, error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return CompletedAuthorization{}, ErrStateNotFound
	}

	now := g.clock().UTC()
	record, err := g.store.ConsumeOAuthState(ctx, GoogleProvider, hashState(state), now)
	if err != nil {
		return CompletedAuthorization{}, err
	}
	if !now.Before(record.ExpiresAt) {
		return CompletedAuthorization{}, ErrStateExpired
	}

	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, g.httpClient)
	token, err := g.config.Exchange(exchangeCtx, code, oauth2.VerifierOption(record.CodeVerifier))
	if err != nil {
		return CompletedAuthorization{}, fmt.Errorf("exchange google authorization code: %w", err)
	}

	profile, err := g.fetchProfile(ctx, token)
	if err != nil {
		return CompletedAuthorization{}, err
	}
	if !profile.EmailVerified || profile.Email == "" {
		return CompletedAuthorization{}, ErrEmailNotVerified
	}

	return CompletedAuthorization{Profile: profile, RedirectTo: record.RedirectTo}, nil
}

func (g *GoogleAuthenticator) fetchProfile(ctx context.Context, token *oauth2.Token) (GoogleProfile, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, g.userInfoURL, nil)
	if err != nil {
		return GoogleProfile{}, fmt.Errorf("build google profile request: %w", err)
	}
	token.SetAuthHeader(request)
	request.Header.Set("Accept", "application/json")

	response, err := g.httpClient.Do(request)
	if err != nil {
		return GoogleProfile{}, fmt.Errorf("read google profile: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()

	if response.StatusCode != http.StatusOK {
		return GoogleProfile{}, fmt.Errorf("google profile endpoint returned %d", response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxUserInfoResponseSize))
	if err != nil {
		return GoogleProfile{}, fmt.Errorf("read google profile body: %w", err)
	}

	var payload struct {
		Subject       string       `json:"sub"`
		Email         string       `json:"email"`
		EmailVerified flexibleBool `json:"email_verified"`
		Name          string       `json:"name"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return GoogleProfile{}, fmt.Errorf("decode google profile: %w", err)
	}

	return GoogleProfile{
		Subject:       payload.Subject,
		Email:         strings.ToLower(strings.TrimSpace(payload.Email)),
		EmailVerified: bool(payload.EmailVerified),
		Name:          strings.TrimSpace(payload.Name),
	}, nil
}

type flexibleBool bool

func (b *flexibleBool) UnmarshalJSON(data []byte) error {
	var asBool bool
	if err := json.Unmarshal(data, &asBool); err == nil {
		*b = flexibleBool(asBool)
		return nil
	}
	var asString string
	if err := json.Unmarshal(data, &asString); err != nil {
		return fmt.Errorf("decode boolean field: %w", err)
	}
	*b = flexibleBool(strings.EqualFold(asString, "true"))
	return nil
}

func SafeRedirectPath(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || !strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "//") {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" {
		return ""
	}
	return parsed.RequestURI()
}

func hashState(state string) string {
	digest := sha256.Sum256([]byte(state))
	return hex.EncodeToString(digest[:])
}
