package auth_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/auth"
)

const testSecret = "test-secret-that-is-long-enough-0123456789"

func mustIssuer(t *testing.T, opts ...auth.TokenIssuerOption) *auth.TokenIssuer {
	t.Helper()
	issuer, err := auth.NewTokenIssuer([]byte(testSecret), opts...)
	if err != nil {
		t.Fatalf("new token issuer: %v", err)
	}
	return issuer
}

func TestNewTokenIssuerRejectsShortSecrets(t *testing.T) {
	if _, err := auth.NewTokenIssuer([]byte("too-short")); !errors.Is(err, auth.ErrWeakSecret) {
		t.Fatalf("got %v, want ErrWeakSecret", err)
	}
}

func TestIssueAndVerifyRoundTrip(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	issuer := mustIssuer(t, auth.WithTokenClock(func() time.Time { return now }))

	userID := uuid.New()
	workspaceID := uuid.New()

	issued, err := issuer.Issue(userID, workspaceID, auth.RoleAdmin)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if issued.Token == "" {
		t.Fatal("empty token")
	}
	if got, want := issued.ExpiresAt, now.Add(auth.DefaultAccessTokenTTL); !got.Equal(want) {
		t.Fatalf("expires at %v, want %v", got, want)
	}

	claims, err := issuer.Verify(issued.Token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if claims.UserID != userID {
		t.Fatalf("sub %v, want %v", claims.UserID, userID)
	}
	if claims.WorkspaceID != workspaceID {
		t.Fatalf("ws %v, want %v", claims.WorkspaceID, workspaceID)
	}
	if claims.Role != auth.RoleAdmin {
		t.Fatalf("role %q, want admin", claims.Role)
	}
	if claims.TokenID == "" {
		t.Fatal("empty jti")
	}
	if !claims.ExpiresAt.Equal(issued.ExpiresAt) {
		t.Fatalf("claims expiry %v, want %v", claims.ExpiresAt, issued.ExpiresAt)
	}
}

func TestIssueUsesTheDocumentedClaimNames(t *testing.T) {
	issuer := mustIssuer(t)
	userID := uuid.New()
	workspaceID := uuid.New()

	issued, err := issuer.Issue(userID, workspaceID, auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	parts := strings.Split(issued.Token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts, want 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	for _, name := range []string{"sub", "ws", "role", "jti", "exp", "iat"} {
		if _, ok := body[name]; !ok {
			t.Fatalf("claim %q missing from %v", name, body)
		}
	}
	if body["sub"] != userID.String() {
		t.Fatalf("sub %v, want %v", body["sub"], userID)
	}
	if body["ws"] != workspaceID.String() {
		t.Fatalf("ws %v, want %v", body["ws"], workspaceID)
	}

	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var head map[string]any
	if err := json.Unmarshal(header, &head); err != nil {
		t.Fatalf("decode header json: %v", err)
	}
	if head["alg"] != "HS256" {
		t.Fatalf("alg %v, want HS256", head["alg"])
	}
}

func TestIssueGeneratesADistinctTokenID(t *testing.T) {
	issuer := mustIssuer(t)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleOwner)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		claims, err := issuer.Verify(issued.Token)
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if seen[claims.TokenID] {
			t.Fatalf("duplicate jti %q", claims.TokenID)
		}
		seen[claims.TokenID] = true
	}
}

func TestIssueRejectsAnUnknownRole(t *testing.T) {
	issuer := mustIssuer(t)
	if _, err := issuer.Issue(uuid.New(), uuid.New(), auth.Role("superuser")); !errors.Is(err, auth.ErrInvalidRole) {
		t.Fatalf("got %v, want ErrInvalidRole", err)
	}
}

func TestIssueRejectsAZeroUserID(t *testing.T) {
	issuer := mustIssuer(t)
	if _, err := issuer.Issue(uuid.Nil, uuid.New(), auth.RoleMember); err == nil {
		t.Fatal("expected an error for a zero user id")
	}
}

func TestVerifyRejectsAnExpiredToken(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	current := now
	issuer := mustIssuer(t, auth.WithTokenClock(func() time.Time { return current }))

	issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	current = now.Add(auth.DefaultAccessTokenTTL + time.Minute)
	if _, err := issuer.Verify(issued.Token); !errors.Is(err, auth.ErrExpiredToken) {
		t.Fatalf("got %v, want ErrExpiredToken", err)
	}
}

func TestVerifyAcceptsATokenUntilItExpires(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	current := now
	issuer := mustIssuer(t, auth.WithTokenClock(func() time.Time { return current }))

	issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	current = now.Add(auth.DefaultAccessTokenTTL - time.Second)
	if _, err := issuer.Verify(issued.Token); err != nil {
		t.Fatalf("verify just before expiry: %v", err)
	}
}

func TestVerifyRejectsAnotherSecret(t *testing.T) {
	issuer := mustIssuer(t)
	other, err := auth.NewTokenIssuer([]byte("a-completely-different-secret-0123456789"))
	if err != nil {
		t.Fatalf("new token issuer: %v", err)
	}

	issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if _, err := other.Verify(issued.Token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsTamperedClaims(t *testing.T) {
	issuer := mustIssuer(t)
	issued, err := issuer.Issue(uuid.New(), uuid.New(), auth.RoleMember)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}

	parts := strings.Split(issued.Token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(payload, &body); err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	body["role"] = string(auth.RoleOwner)
	edited, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode claims: %v", err)
	}
	parts[1] = base64.RawURLEncoding.EncodeToString(edited)

	if _, err := issuer.Verify(strings.Join(parts, ".")); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsTheNoneAlgorithm(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"` + uuid.NewString() + `","ws":"` + uuid.NewString() +
			`","role":"owner","jti":"x","exp":` + strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10) + `}`))

	issuer := mustIssuer(t)
	if _, err := issuer.Verify(header + "." + payload + "."); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	issuer := mustIssuer(t)
	for _, token := range []string{"", "   ", "not-a-token", "a.b.c", strings.Repeat("x", 4096)} {
		if _, err := issuer.Verify(token); !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("token %q: got %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestVerifyRejectsAnUnparsableSubject(t *testing.T) {
	issuer := mustIssuer(t)
	token, err := signHS256(t, map[string]any{
		"sub":  "not-a-uuid",
		"ws":   uuid.NewString(),
		"role": "member",
		"jti":  "abc",
		"iss":  "afterword",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := issuer.Verify(token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestVerifyRejectsAnUnknownRoleClaim(t *testing.T) {
	issuer := mustIssuer(t)
	token, err := signHS256(t, map[string]any{
		"sub":  uuid.NewString(),
		"ws":   uuid.NewString(),
		"role": "root",
		"jti":  "abc",
		"iss":  "afterword",
		"exp":  time.Now().Add(time.Hour).Unix(),
		"iat":  time.Now().Unix(),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if _, err := issuer.Verify(token); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("got %v, want ErrInvalidToken", err)
	}
}

func TestRoleRanking(t *testing.T) {
	cases := []struct {
		role    auth.Role
		minimum auth.Role
		want    bool
	}{
		{auth.RoleOwner, auth.RoleOwner, true},
		{auth.RoleOwner, auth.RoleAdmin, true},
		{auth.RoleOwner, auth.RoleMember, true},
		{auth.RoleAdmin, auth.RoleOwner, false},
		{auth.RoleAdmin, auth.RoleAdmin, true},
		{auth.RoleAdmin, auth.RoleMember, true},
		{auth.RoleMember, auth.RoleAdmin, false},
		{auth.RoleMember, auth.RoleMember, true},
		{auth.Role("nonsense"), auth.RoleMember, false},
	}
	for _, tc := range cases {
		if got := tc.role.AtLeast(tc.minimum); got != tc.want {
			t.Fatalf("Role(%q).AtLeast(%q) = %v, want %v", tc.role, tc.minimum, got, tc.want)
		}
	}
}

func signHS256(t *testing.T, claims map[string]any) (string, error) {
	t.Helper()
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims(claims)).SignedString([]byte(testSecret))
}
