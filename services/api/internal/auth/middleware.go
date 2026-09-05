package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/httpx"
)

var ErrNotMember = errors.New("auth: user is not a member of that workspace")

const (
	WorkspaceHeader        = "X-Workspace-Id"
	DefaultWorkspaceParam  = "workspaceID"
	authorizationScheme    = "bearer"
	unauthorizedMessage    = "Sign in to continue."
	tokenExpiredMessage    = "The access token has expired."
	forbiddenMessage       = "You do not have access to that workspace."
	insufficientRoleText   = "Your role does not allow that action."
	workspaceMissingText   = "A workspace id is required."
	workspaceMalformedText = "The workspace id is not a valid identifier."
	internalMessage        = "Something went wrong."
)

type Identity struct {
	UserID      uuid.UUID
	WorkspaceID uuid.UUID
	Role        Role
	TokenID     string
}

type Membership struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Role        Role
}

type MembershipLoader interface {
	LoadMembership(ctx context.Context, workspaceID, userID uuid.UUID) (Membership, error)
}

type contextKey int

const (
	identityContextKey contextKey = iota
	membershipContextKey
)

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityContextKey).(Identity)
	return identity, ok
}

func MembershipFromContext(ctx context.Context) (Membership, bool) {
	membership, ok := ctx.Value(membershipContextKey).(Membership)
	return membership, ok
}

func ContextWithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityContextKey, identity)
}

func ContextWithMembership(ctx context.Context, membership Membership) context.Context {
	return context.WithValue(ctx, membershipContextKey, membership)
}

type MiddlewareOptions struct {
	Issuer         *TokenIssuer
	Memberships    MembershipLoader
	WorkspaceParam string
}

type Middleware struct {
	issuer         *TokenIssuer
	memberships    MembershipLoader
	workspaceParam string
}

func NewMiddleware(options MiddlewareOptions) (*Middleware, error) {
	if options.Issuer == nil {
		return nil, errors.New("auth: a token issuer is required")
	}
	if options.Memberships == nil {
		return nil, errors.New("auth: a membership loader is required")
	}
	param := options.WorkspaceParam
	if strings.TrimSpace(param) == "" {
		param = DefaultWorkspaceParam
	}
	return &Middleware{issuer: options.Issuer, memberships: options.Memberships, workspaceParam: param}, nil
}

func (m *Middleware) RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w, r, httpx.CodeUnauthorized, unauthorizedMessage)
			return
		}

		claims, err := m.issuer.Verify(token)
		if err != nil {
			if errors.Is(err, ErrExpiredToken) {
				unauthorized(w, r, httpx.CodeTokenExpired, tokenExpiredMessage)
				return
			}
			unauthorized(w, r, httpx.CodeUnauthorized, unauthorizedMessage)
			return
		}

		identity := Identity{
			UserID:      claims.UserID,
			WorkspaceID: claims.WorkspaceID,
			Role:        claims.Role,
			TokenID:     claims.TokenID,
		}
		next.ServeHTTP(w, r.WithContext(ContextWithIdentity(r.Context(), identity)))
	})
}

func (m *Middleware) RequireWorkspace(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := IdentityFromContext(r.Context())
		if !ok {
			hlog.FromRequest(r).Error().Msg("require workspace ran without require auth")
			unauthorized(w, r, httpx.CodeUnauthorized, unauthorizedMessage)
			return
		}

		raw := strings.TrimSpace(chi.URLParam(r, m.workspaceParam))
		if raw == "" {
			raw = strings.TrimSpace(r.Header.Get(WorkspaceHeader))
		}
		if raw == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, workspaceMissingText)
			return
		}
		workspaceID, err := uuid.Parse(raw)
		if err != nil || workspaceID == uuid.Nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.CodeInvalidRequest, workspaceMalformedText)
			return
		}

		membership, err := m.memberships.LoadMembership(r.Context(), workspaceID, identity.UserID)
		if err != nil {
			if errors.Is(err, ErrNotMember) {
				httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, forbiddenMessage)
				return
			}
			hlog.FromRequest(r).Error().Err(err).Msg("load membership failed")
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternalError, internalMessage)
			return
		}
		if !membership.Role.Valid() {
			hlog.FromRequest(r).Error().Str("role", string(membership.Role)).Msg("membership carries an unknown role")
			httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, forbiddenMessage)
			return
		}

		next.ServeHTTP(w, r.WithContext(ContextWithMembership(r.Context(), membership)))
	})
}

func (m *Middleware) RequireRole(minimum Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			membership, ok := MembershipFromContext(r.Context())
			if !ok {
				hlog.FromRequest(r).Error().Msg("require role ran without require workspace")
				httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, forbiddenMessage)
				return
			}
			if !membership.Role.AtLeast(minimum) {
				httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, insufficientRoleText)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) (string, bool) {
	trimmed := strings.TrimSpace(header)
	if trimmed == "" {
		return "", false
	}
	scheme, token, found := strings.Cut(trimmed, " ")
	if !found {
		return "", false
	}
	if !strings.EqualFold(scheme, authorizationScheme) {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

func unauthorized(w http.ResponseWriter, r *http.Request, code, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="afterword"`)
	httpx.WriteError(w, r, http.StatusUnauthorized, code, message)
}
