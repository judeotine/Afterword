package auth_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type memoryMembershipLoader struct {
	memberships map[string]auth.Role
	err         error
	calls       int
}

func newMemoryMembershipLoader() *memoryMembershipLoader {
	return &memoryMembershipLoader{memberships: map[string]auth.Role{}}
}

func (l *memoryMembershipLoader) add(workspaceID, userID uuid.UUID, role auth.Role) {
	l.memberships[workspaceID.String()+"/"+userID.String()] = role
}

func (l *memoryMembershipLoader) LoadMembership(_ context.Context, workspaceID, userID uuid.UUID) (auth.Membership, error) {
	l.calls++
	if l.err != nil {
		return auth.Membership{}, l.err
	}
	role, ok := l.memberships[workspaceID.String()+"/"+userID.String()]
	if !ok {
		return auth.Membership{}, auth.ErrNotMember
	}
	return auth.Membership{WorkspaceID: workspaceID, UserID: userID, Role: role}, nil
}

type middlewareFixture struct {
	middleware *auth.Middleware
	issuer     *auth.TokenIssuer
	loader     *memoryMembershipLoader
}

func newMiddlewareFixture(t *testing.T) *middlewareFixture {
	t.Helper()
	issuer := mustIssuer(t)
	loader := newMemoryMembershipLoader()
	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: issuer, Memberships: loader})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}
	return &middlewareFixture{middleware: middleware, issuer: issuer, loader: loader}
}

func (f *middlewareFixture) token(t *testing.T, userID, workspaceID uuid.UUID, role auth.Role) string {
	t.Helper()
	issued, err := f.issuer.Issue(userID, workspaceID, role)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	return issued.Token
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var envelope httpx.ErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode error envelope from %q: %v", body, err)
	}
	return envelope.Error.Code
}

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestRequireAuthRejectsAMissingOrMalformedHeader(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	handler := fixture.middleware.RequireAuth(okHandler())

	cases := []string{"", "Bearer", "Bearer ", "Basic abcdef", "Token abcdef", "Bearer a b", "bearerabc"}
	for _, header := range cases {
		request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("header %q: status %d, want 401", header, recorder.Code)
		}
		if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeUnauthorized {
			t.Fatalf("header %q: code %q", header, got)
		}
	}
}

func TestRequireAuthAcceptsAValidToken(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	userID := uuid.New()
	workspaceID := uuid.New()

	var seen auth.Identity
	handler := fixture.middleware.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok {
			t.Error("no identity in the request context")
		}
		seen = identity
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, userID, workspaceID, auth.RoleAdmin))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status %d, want 204", recorder.Code)
	}
	if seen.UserID != userID {
		t.Fatalf("identity user %v, want %v", seen.UserID, userID)
	}
	if seen.WorkspaceID != workspaceID || seen.Role != auth.RoleAdmin {
		t.Fatalf("identity %+v", seen)
	}
	if seen.TokenID == "" {
		t.Fatal("identity has no token id")
	}
}

func TestRequireAuthIsCaseInsensitiveAboutTheScheme(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	handler := fixture.middleware.RequireAuth(okHandler())

	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.Header.Set("Authorization", "bearer "+fixture.token(t, uuid.New(), uuid.New(), auth.RoleMember))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", recorder.Code)
	}
}

func TestRequireAuthRejectsAnExpiredToken(t *testing.T) {
	loader := newMemoryMembershipLoader()
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	current := now
	issuer := mustIssuer(t, auth.WithTokenClock(func() time.Time { return current }))
	middleware, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: issuer, Memberships: loader})
	if err != nil {
		t.Fatalf("new middleware: %v", err)
	}
	issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	current = now.Add(2 * auth.DefaultAccessTokenTTL)

	request := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+issued.Token)
	recorder := httptest.NewRecorder()
	middleware.RequireAuth(okHandler()).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", recorder.Code)
	}
	if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeTokenExpired {
		t.Fatalf("code %q, want %q", got, httpx.CodeTokenExpired)
	}
}

func TestRequireWorkspaceLoadsTheRoleFromTheDatabaseAndNotTheToken(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	userID := uuid.New()
	workspaceID := uuid.New()
	fixture.loader.add(workspaceID, userID, auth.RoleMember)

	var seen auth.Membership
	handler := fixture.middleware.RequireAuth(fixture.middleware.RequireWorkspace(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			membership, ok := auth.MembershipFromContext(r.Context())
			if !ok {
				t.Error("no membership in the request context")
			}
			seen = membership
			w.WriteHeader(http.StatusOK)
		})))

	request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, userID, workspaceID, auth.RoleOwner))
	request.Header.Set("X-Workspace-Id", workspaceID.String())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", recorder.Code, recorder.Body)
	}
	if seen.Role != auth.RoleMember {
		t.Fatalf("role %q, want the database role member", seen.Role)
	}
	if fixture.loader.calls != 1 {
		t.Fatalf("membership was loaded %d times, want 1", fixture.loader.calls)
	}
}

func TestRequireWorkspacePrefersThePathParameter(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	userID := uuid.New()
	pathWorkspace := uuid.New()
	headerWorkspace := uuid.New()
	fixture.loader.add(pathWorkspace, userID, auth.RoleAdmin)
	fixture.loader.add(headerWorkspace, userID, auth.RoleOwner)

	var seen auth.Membership
	router := chi.NewRouter()
	router.With(fixture.middleware.RequireAuth, fixture.middleware.RequireWorkspace).
		Get("/v1/workspaces/{workspaceID}/members", func(w http.ResponseWriter, r *http.Request) {
			seen, _ = auth.MembershipFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

	request := httptest.NewRequest(http.MethodGet, "/v1/workspaces/"+pathWorkspace.String()+"/members", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, userID, headerWorkspace, auth.RoleOwner))
	request.Header.Set("X-Workspace-Id", headerWorkspace.String())
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", recorder.Code, recorder.Body)
	}
	if seen.WorkspaceID != pathWorkspace {
		t.Fatalf("workspace %v, want the path parameter %v", seen.WorkspaceID, pathWorkspace)
	}
	if seen.Role != auth.RoleAdmin {
		t.Fatalf("role %q, want admin", seen.Role)
	}
}

func TestRequireWorkspaceRefusesForeignWorkspacesWithoutLeakingExistence(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	userID := uuid.New()
	ownWorkspace := uuid.New()
	otherWorkspace := uuid.New()
	fixture.loader.add(ownWorkspace, userID, auth.RoleOwner)
	fixture.loader.add(otherWorkspace, uuid.New(), auth.RoleOwner)

	handler := fixture.middleware.RequireAuth(fixture.middleware.RequireWorkspace(okHandler()))
	token := fixture.token(t, userID, ownWorkspace, auth.RoleOwner)

	bodies := map[string]string{}
	for _, workspaceID := range []uuid.UUID{otherWorkspace, uuid.New()} {
		request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Workspace-Id", workspaceID.String())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusForbidden {
			t.Fatalf("workspace %v: status %d, want 403", workspaceID, recorder.Code)
		}
		if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeForbidden {
			t.Fatalf("code %q, want %q", got, httpx.CodeForbidden)
		}
		bodies[workspaceID.String()] = recorder.Body.String()
	}

	var previous string
	for _, body := range bodies {
		if previous != "" && body != previous {
			t.Fatalf("an existing workspace answers differently from a missing one: %q vs %q", body, previous)
		}
		previous = body
	}
}

func TestRequireWorkspaceIgnoresTheWorkspaceClaimInTheToken(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	userID := uuid.New()
	claimed := uuid.New()
	fixture.loader.add(claimed, uuid.New(), auth.RoleOwner)

	handler := fixture.middleware.RequireAuth(fixture.middleware.RequireWorkspace(okHandler()))
	request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, userID, claimed, auth.RoleOwner))
	request.Header.Set("X-Workspace-Id", claimed.String())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
}

func TestRequireWorkspaceRejectsAMissingOrMalformedIdentifier(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	handler := fixture.middleware.RequireAuth(fixture.middleware.RequireWorkspace(okHandler()))
	token := fixture.token(t, uuid.New(), uuid.New(), auth.RoleMember)

	for _, header := range []string{"", "not-a-uuid", "  "} {
		request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		if header != "" {
			request.Header.Set("X-Workspace-Id", header)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("header %q: status %d, want 400", header, recorder.Code)
		}
		if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeInvalidRequest {
			t.Fatalf("header %q: code %q", header, got)
		}
	}
}

func TestRequireWorkspaceWithoutAnIdentityIsUnauthorized(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
	request.Header.Set("X-Workspace-Id", uuid.NewString())
	recorder := httptest.NewRecorder()
	fixture.middleware.RequireWorkspace(okHandler()).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", recorder.Code)
	}
}

func TestRequireWorkspaceReportsLoaderFailuresAsInternalErrors(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	fixture.loader.err = errors.New("connection refused to db-01.internal")

	handler := fixture.middleware.RequireAuth(fixture.middleware.RequireWorkspace(okHandler()))
	request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
	request.Header.Set("Authorization", "Bearer "+fixture.token(t, uuid.New(), uuid.New(), auth.RoleMember))
	request.Header.Set("X-Workspace-Id", uuid.NewString())
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", recorder.Code)
	}
	if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeInternalError {
		t.Fatalf("code %q", got)
	}
	if body := recorder.Body.String(); strings.Contains(body, "db-01.internal") {
		t.Fatalf("the driver error leaked into the response: %s", body)
	}
}

func TestRequireRoleMatrix(t *testing.T) {
	cases := []struct {
		actual  auth.Role
		minimum auth.Role
		want    int
	}{
		{auth.RoleOwner, auth.RoleOwner, http.StatusOK},
		{auth.RoleAdmin, auth.RoleOwner, http.StatusForbidden},
		{auth.RoleMember, auth.RoleOwner, http.StatusForbidden},
		{auth.RoleOwner, auth.RoleAdmin, http.StatusOK},
		{auth.RoleAdmin, auth.RoleAdmin, http.StatusOK},
		{auth.RoleMember, auth.RoleAdmin, http.StatusForbidden},
		{auth.RoleOwner, auth.RoleMember, http.StatusOK},
		{auth.RoleAdmin, auth.RoleMember, http.StatusOK},
		{auth.RoleMember, auth.RoleMember, http.StatusOK},
	}

	for _, tc := range cases {
		fixture := newMiddlewareFixture(t)
		userID := uuid.New()
		workspaceID := uuid.New()
		fixture.loader.add(workspaceID, userID, tc.actual)

		handler := fixture.middleware.RequireAuth(
			fixture.middleware.RequireWorkspace(
				fixture.middleware.RequireRole(tc.minimum)(okHandler())))

		request := httptest.NewRequest(http.MethodPost, "/v1/workspaces/x/invites", nil)
		request.Header.Set("Authorization", "Bearer "+fixture.token(t, userID, workspaceID, auth.RoleOwner))
		request.Header.Set("X-Workspace-Id", workspaceID.String())
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)

		if recorder.Code != tc.want {
			t.Fatalf("role %q against minimum %q: status %d, want %d", tc.actual, tc.minimum, recorder.Code, tc.want)
		}
		if tc.want == http.StatusForbidden {
			if got := errorCode(t, recorder.Body.Bytes()); got != httpx.CodeForbidden {
				t.Fatalf("code %q", got)
			}
		}
	}
}

func TestRequireRoleWithoutAMembershipIsForbidden(t *testing.T) {
	fixture := newMiddlewareFixture(t)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/meetings", nil)
	fixture.middleware.RequireRole(auth.RoleAdmin)(okHandler()).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", recorder.Code)
	}
}

func TestNewMiddlewareRequiresItsDependencies(t *testing.T) {
	issuer := mustIssuer(t)
	if _, err := auth.NewMiddleware(auth.MiddlewareOptions{Memberships: newMemoryMembershipLoader()}); err == nil {
		t.Fatal("expected an error without an issuer")
	}
	if _, err := auth.NewMiddleware(auth.MiddlewareOptions{Issuer: issuer}); err == nil {
		t.Fatal("expected an error without a membership loader")
	}
}
