//go:build integration

package api_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/api"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

func TestOTPSignUpRefreshAndLogout(t *testing.T) {
	harness := newHarness(t)

	sent := harness.sendCode("Person@Example.com")
	if sent.Status != http.StatusAccepted {
		t.Fatalf("send: status %d, body %s", sent.Status, sent.Body)
	}
	email := harness.sender.lastEmail(t)
	if email.To != "person@example.com" {
		t.Fatalf("email addressed to %q", email.To)
	}
	code := codePattern.FindString(email.Text)

	if strings.Contains(string(sent.Body), code) {
		t.Fatal("the send response leaked the code")
	}

	verified := harness.do(http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"channel":     "email",
		"destination": "person@example.com",
		"code":        code,
		"device_id":   "desktop-1",
	})
	if verified.Status != http.StatusOK {
		t.Fatalf("verify: status %d, body %s", verified.Status, verified.Body)
	}

	var first session
	verified.decode(t, &first)
	if first.AccessToken == "" || first.RefreshToken == "" {
		t.Fatalf("session %+v", first)
	}
	if first.TokenType != "Bearer" || first.ExpiresIn != 900 {
		t.Fatalf("session %+v", first)
	}
	if first.User.Email != "person@example.com" {
		t.Fatalf("user %+v", first.User)
	}
	if first.Workspace.ID == "" || first.Workspace.Role != string(auth.RoleOwner) {
		t.Fatalf("workspace %+v", first.Workspace)
	}

	cookie := verified.cookie(api.RefreshCookieName)
	if cookie == nil || cookie.Value != first.RefreshToken || !cookie.HttpOnly {
		t.Fatalf("refresh cookie %+v", cookie)
	}

	me := harness.do(http.MethodGet, "/v1/me", nil, withBearer(first.AccessToken))
	if me.Status != http.StatusOK {
		t.Fatalf("me: status %d, body %s", me.Status, me.Body)
	}
	var profile struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"user"`
		Workspaces []struct {
			ID   string `json:"id"`
			Role string `json:"role"`
		} `json:"workspaces"`
	}
	me.decode(t, &profile)
	if profile.User.ID != first.User.ID {
		t.Fatalf("me returned user %q, want %q", profile.User.ID, first.User.ID)
	}
	if len(profile.Workspaces) != 1 || profile.Workspaces[0].Role != string(auth.RoleOwner) {
		t.Fatalf("workspaces %+v", profile.Workspaces)
	}

	refreshed := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if refreshed.Status != http.StatusOK {
		t.Fatalf("refresh: status %d, body %s", refreshed.Status, refreshed.Body)
	}
	var second session
	refreshed.decode(t, &second)
	if second.RefreshToken == first.RefreshToken {
		t.Fatal("the refresh token was not rotated")
	}
	if second.User.ID != first.User.ID || second.Workspace.ID != first.Workspace.ID {
		t.Fatalf("second session %+v", second)
	}

	replay := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": first.RefreshToken,
	})
	if replay.Status != http.StatusUnauthorized {
		t.Fatalf("replay: status %d, body %s", replay.Status, replay.Body)
	}
	if got := replay.errorCode(t); got != httpx.CodeUnauthorized {
		t.Fatalf("replay code %q", got)
	}

	afterReuse := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": second.RefreshToken,
	})
	if afterReuse.Status != http.StatusUnauthorized {
		t.Fatalf("the family should have been revoked: status %d, body %s", afterReuse.Status, afterReuse.Body)
	}

	third := harness.signIn("person@example.com")
	if third.User.ID != first.User.ID {
		t.Fatalf("a second sign-in created user %q, want %q", third.User.ID, first.User.ID)
	}
	if third.Workspace.ID != first.Workspace.ID {
		t.Fatalf("a second sign-in created workspace %q", third.Workspace.ID)
	}

	loggedOut := harness.do(http.MethodPost, "/v1/auth/logout", map[string]string{
		"refresh_token": third.RefreshToken,
	})
	if loggedOut.Status != http.StatusNoContent {
		t.Fatalf("logout: status %d, body %s", loggedOut.Status, loggedOut.Body)
	}
	if cleared := loggedOut.cookie(api.RefreshCookieName); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("logout did not clear the cookie: %+v", cleared)
	}

	afterLogout := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": third.RefreshToken,
	})
	if afterLogout.Status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: status %d", afterLogout.Status)
	}

	repeatedLogout := harness.do(http.MethodPost, "/v1/auth/logout", map[string]string{
		"refresh_token": third.RefreshToken,
	})
	if repeatedLogout.Status != http.StatusNoContent {
		t.Fatalf("logout is not idempotent: status %d", repeatedLogout.Status)
	}
}

func TestRefreshAcceptsTheCookie(t *testing.T) {
	harness := newHarness(t)
	first := harness.signIn("person@example.com")

	refreshed := harness.do(http.MethodPost, "/v1/auth/refresh", nil, func(r *http.Request) {
		r.AddCookie(&http.Cookie{Name: api.RefreshCookieName, Value: first.RefreshToken})
	})
	if refreshed.Status != http.StatusOK {
		t.Fatalf("refresh by cookie: status %d, body %s", refreshed.Status, refreshed.Body)
	}
}

func TestRefreshRejectsAWorkspaceTheUserIsNotIn(t *testing.T) {
	harness := newHarness(t)
	owner := harness.signIn("owner@example.com")
	stranger := harness.signIn("stranger@example.com")

	refreshed := harness.do(http.MethodPost, "/v1/auth/refresh", map[string]string{
		"refresh_token": stranger.RefreshToken,
		"workspace_id":  owner.Workspace.ID,
	})
	if refreshed.Status != http.StatusForbidden {
		t.Fatalf("status %d, body %s", refreshed.Status, refreshed.Body)
	}
	if got := refreshed.errorCode(t); got != httpx.CodeForbidden {
		t.Fatalf("code %q", got)
	}
}

func TestOTPSendIsRateLimitedByDestination(t *testing.T) {
	harness := newHarness(t)

	for i := 0; i < auth.DefaultSendsPerDestination; i++ {
		if sent := harness.sendCode("person@example.com"); sent.Status != http.StatusAccepted {
			t.Fatalf("send %d: status %d", i, sent.Status)
		}
	}
	limited := harness.sendCode("person@example.com")
	if limited.Status != http.StatusTooManyRequests {
		t.Fatalf("status %d, body %s", limited.Status, limited.Body)
	}
	if got := limited.errorCode(t); got != httpx.CodeRateLimited {
		t.Fatalf("code %q", got)
	}
	if limited.Body != nil && strings.TrimSpace(string(limited.Body)) == "" {
		t.Fatal("empty error body")
	}
}

func TestOTPSendIsRateLimitedByAddress(t *testing.T) {
	harness := newHarness(t)

	for i := 0; i < auth.DefaultSendsPerIP; i++ {
		destination := uniqueAddress(i)
		if sent := harness.sendCode(destination, withRemoteIP("198.51.100.10")); sent.Status != http.StatusAccepted {
			t.Fatalf("send %d: status %d", i, sent.Status)
		}
	}

	limited := harness.sendCode(uniqueAddress(999), withRemoteIP("198.51.100.10"))
	if limited.Status != http.StatusTooManyRequests {
		t.Fatalf("status %d, body %s", limited.Status, limited.Body)
	}

	elsewhere := harness.sendCode(uniqueAddress(1000), withRemoteIP("198.51.100.11"))
	if elsewhere.Status != http.StatusAccepted {
		t.Fatalf("another address was limited: status %d", elsewhere.Status)
	}
}

func TestVerifyRejectsBadInput(t *testing.T) {
	harness := newHarness(t)
	harness.sendCode("person@example.com")

	cases := []struct {
		name string
		body map[string]string
		want int
	}{
		{"wrong code", map[string]string{"channel": "email", "destination": "person@example.com", "code": "000001"}, http.StatusUnauthorized},
		{"unknown destination", map[string]string{"channel": "email", "destination": "nobody@example.com", "code": "123456"}, http.StatusUnauthorized},
		{"bad channel", map[string]string{"channel": "smoke-signal", "destination": "person@example.com", "code": "123456"}, http.StatusBadRequest},
		{"bad destination", map[string]string{"channel": "email", "destination": "not-an-email", "code": "123456"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		got := harness.do(http.MethodPost, "/v1/auth/otp/verify", tc.body)
		if got.Status != tc.want {
			t.Fatalf("%s: status %d, want %d, body %s", tc.name, got.Status, tc.want, got.Body)
		}
	}

	malformed := harness.do(http.MethodPost, "/v1/auth/otp/verify", nil, func(r *http.Request) {
		r.Header.Set("Content-Type", "application/json")
	})
	if malformed.Status != http.StatusBadRequest {
		t.Fatalf("empty body: status %d", malformed.Status)
	}
}

func TestOTPVerifyLocksOutAfterTooManyAttempts(t *testing.T) {
	harness := newHarness(t)
	harness.sendCode("person@example.com")
	code := codePattern.FindString(harness.sender.lastEmail(t).Text)

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}

	for i := 0; i < auth.DefaultOTPMaxAttempts-1; i++ {
		got := harness.do(http.MethodPost, "/v1/auth/otp/verify", map[string]string{
			"channel": "email", "destination": "person@example.com", "code": wrong,
		})
		if got.Status != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status %d", i, got.Status)
		}
	}

	final := harness.do(http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"channel": "email", "destination": "person@example.com", "code": wrong,
	})
	if final.Status != http.StatusTooManyRequests {
		t.Fatalf("status %d, body %s", final.Status, final.Body)
	}

	afterLockout := harness.do(http.MethodPost, "/v1/auth/otp/verify", map[string]string{
		"channel": "email", "destination": "person@example.com", "code": code,
	})
	if afterLockout.Status != http.StatusTooManyRequests {
		t.Fatalf("the right code after lockout: status %d", afterLockout.Status)
	}
}

func TestGoogleRoutesReportWhenNotConfigured(t *testing.T) {
	harness := newHarness(t)

	for _, path := range []string{"/v1/auth/google/start", "/v1/auth/google/callback?code=x&state=y"} {
		got := harness.do(http.MethodGet, path, nil)
		if got.Status != http.StatusServiceUnavailable {
			t.Fatalf("%s: status %d, body %s", path, got.Status, got.Body)
		}
	}
}

func TestProtectedRoutesRequireAuthentication(t *testing.T) {
	harness := newHarness(t)
	workspaceID := uuid.NewString()

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/me"},
		{http.MethodGet, "/v1/workspaces"},
		{http.MethodPost, "/v1/workspaces"},
		{http.MethodGet, "/v1/workspaces/" + workspaceID + "/"},
		{http.MethodGet, "/v1/workspaces/" + workspaceID + "/members"},
		{http.MethodPost, "/v1/workspaces/" + workspaceID + "/invites"},
		{http.MethodGet, "/v1/workspaces/" + workspaceID + "/invites"},
		{http.MethodPatch, "/v1/workspaces/" + workspaceID + "/members/" + uuid.NewString()},
		{http.MethodDelete, "/v1/workspaces/" + workspaceID + "/members/" + uuid.NewString()},
		{http.MethodPost, "/v1/invites/some-token/accept"},
	}

	for _, route := range routes {
		anonymous := harness.do(route.method, route.path, nil)
		if anonymous.Status != http.StatusUnauthorized {
			t.Fatalf("%s %s without a token: status %d, body %s", route.method, route.path, anonymous.Status, anonymous.Body)
		}
		if got := anonymous.errorCode(t); got != httpx.CodeUnauthorized {
			t.Fatalf("%s %s: code %q", route.method, route.path, got)
		}

		forged := harness.do(route.method, route.path, nil, withBearer("not.a.token"))
		if forged.Status != http.StatusUnauthorized {
			t.Fatalf("%s %s with a forged token: status %d", route.method, route.path, forged.Status)
		}
	}
}

func uniqueAddress(n int) string {
	return "person" + strconv.Itoa(n) + "@example.com"
}
