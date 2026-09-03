//go:build integration

package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const harnessSecret = "integration-secret-0123456789abcdef"

type captureSender struct {
	mu     sync.Mutex
	emails []auth.EmailMessage
	texts  []string
}

func (s *captureSender) SendEmail(_ context.Context, message auth.EmailMessage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emails = append(s.emails, message)
	return nil
}

func (s *captureSender) SendSMS(_ context.Context, _, text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.texts = append(s.texts, text)
	return nil
}

func (s *captureSender) lastEmail(t *testing.T) auth.EmailMessage {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.emails) == 0 {
		t.Fatal("no email was sent")
	}
	return s.emails[len(s.emails)-1]
}

type harness struct {
	t       *testing.T
	handler http.Handler
	sender  *captureSender
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool := dbtest.New(t)

	store, err := auth.NewStore(pool)
	if err != nil {
		t.Fatalf("new auth store: %v", err)
	}
	accountsService, err := accounts.NewService(pool)
	if err != nil {
		t.Fatalf("new accounts service: %v", err)
	}
	tokens, err := auth.NewTokenIssuer([]byte(harnessSecret))
	if err != nil {
		t.Fatalf("new token issuer: %v", err)
	}
	refresh, err := auth.NewRefreshManager(auth.RefreshManagerOptions{Store: store})
	if err != nil {
		t.Fatalf("new refresh manager: %v", err)
	}
	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: tokens, Memberships: accountsService})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}

	sender := &captureSender{}
	otp, err := auth.NewOTPService(auth.OTPServiceOptions{
		Store:       store,
		EmailSender: sender,
		SMSSender:   sender,
		HashCost:    bcrypt.MinCost,
	})
	if err != nil {
		t.Fatalf("new otp service: %v", err)
	}

	server, err := api.NewServer(api.ServerOptions{
		Accounts:   accountsService,
		OTP:        otp,
		Tokens:     tokens,
		Refresh:    refresh,
		Middleware: middleware,
		Email:      sender,
		AppBaseURL: "http://localhost:3000",
	})
	if err != nil {
		t.Fatalf("new api server: %v", err)
	}

	return &harness{
		t:      t,
		sender: sender,
		handler: httpx.NewRouter(httpx.RouterOptions{
			Logger:        zerolog.Nop(),
			AllowedOrigin: "http://localhost:3000",
			Mount:         server.Routes,
		}),
	}
}

type response struct {
	Status  int
	Body    []byte
	Cookies []*http.Cookie
}

func (r response) decode(t *testing.T, target any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, target); err != nil {
		t.Fatalf("decode %q: %v", r.Body, err)
	}
}

func (r response) errorCode(t *testing.T) string {
	t.Helper()
	var envelope httpx.ErrorEnvelope
	if err := json.Unmarshal(r.Body, &envelope); err != nil {
		t.Fatalf("decode error envelope %q: %v", r.Body, err)
	}
	return envelope.Error.Code
}

func (r response) cookie(name string) *http.Cookie {
	for _, cookie := range r.Cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func (h *harness) do(method, path string, body any, decorate ...func(*http.Request)) response {
	h.t.Helper()

	var reader *bytes.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	} else {
		reader = bytes.NewReader(nil)
	}

	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for _, apply := range decorate {
		apply(request)
	}

	recorder := httptest.NewRecorder()
	h.handler.ServeHTTP(recorder, request)
	result := recorder.Result()
	defer func() {
		_ = result.Body.Close()
	}()

	return response{Status: recorder.Code, Body: recorder.Body.Bytes(), Cookies: result.Cookies()}
}

func withBearer(token string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+token)
	}
}

func withWorkspace(id string) func(*http.Request) {
	return func(r *http.Request) {
		r.Header.Set(auth.WorkspaceHeader, id)
	}
}

func withRemoteIP(ip string) func(*http.Request) {
	return func(r *http.Request) {
		r.RemoteAddr = ip + ":54321"
	}
}

type session struct {
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresAt string `json:"refresh_expires_at"`
	User             struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name"`
	} `json:"user"`
	Workspace struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	} `json:"workspace"`
}

var codePattern = regexp.MustCompile(`\b\d{6}\b`)

func (h *harness) sendCode(destination string, decorate ...func(*http.Request)) response {
	h.t.Helper()
	return h.do(http.MethodPost, "/v1/auth/otp/send", map[string]string{
		"channel":     "email",
		"destination": destination,
	}, decorate...)
}

func (h *harness) signIn(destination string) session {
	h.t.Helper()

	sent := h.sendCode(destination)
	if sent.Status != http.StatusAccepted {
		h.t.Fatalf("send code: status %d, body %s", sent.Status, sent.Body)
	}

	code := codePattern.FindString(h.sender.lastEmail(h.t).Text)
	if code == "" {
		h.t.Fatal("no code in the sent email")
	}

	verified := h.do(http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"channel":     "email",
		"destination": destination,
		"code":        code,
		"device_id":   "integration-test",
	})
	if verified.Status != http.StatusOK {
		h.t.Fatalf("verify code: status %d, body %s", verified.Status, verified.Body)
	}

	var result session
	verified.decode(h.t, &result)
	return result
}
