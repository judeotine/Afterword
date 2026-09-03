package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type sendCodeRequest struct {
	Channel     string `json:"channel"`
	Destination string `json:"destination"`
}

type sendCodeResponse struct {
	Status    string    `json:"status"`
	Channel   string    `json:"channel"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (s *Server) handleSendCode(w http.ResponseWriter, r *http.Request) {
	var body sendCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	channel, err := auth.ParseChannel(body.Channel)
	if err != nil {
		writeValidationError(w, r, "channel must be email or phone.")
		return
	}

	sent, err := s.otp.Send(r.Context(), auth.SendCodeRequest{
		Channel:     channel,
		Destination: body.Destination,
		RequestIP:   requestIP(r),
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidDestination):
			writeValidationError(w, r, "That destination is not a valid email address or phone number.")
		case errors.Is(err, auth.ErrInvalidChannel):
			writeValidationError(w, r, "channel must be email or phone.")
		case errors.Is(err, auth.ErrRateLimited):
			writeRateLimited(w, r, auth.DefaultDestinationWindow, "Too many codes have been requested. Try again later.")
		case errors.Is(err, auth.ErrNotConfigured):
			hlog.FromRequest(r).Error().Err(err).Msg("verification code sender is not configured")
			httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured,
				"Sign-in over that channel is not available yet.")
		default:
			s.writeInternalError(w, r, err)
		}
		return
	}

	httpx.WriteJSON(w, r, http.StatusAccepted, sendCodeResponse{
		Status:    "sent",
		Channel:   string(sent.Channel),
		ExpiresAt: sent.ExpiresAt,
	})
}

type verifyCodeRequest struct {
	Channel     string `json:"channel"`
	Destination string `json:"destination"`
	Code        string `json:"code"`
	DeviceID    string `json:"device_id"`
}

func (s *Server) handleVerifyCode(w http.ResponseWriter, r *http.Request) {
	var body verifyCodeRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	channel, err := auth.ParseChannel(body.Channel)
	if err != nil {
		writeValidationError(w, r, "channel must be email or phone.")
		return
	}

	contact, err := s.otp.Verify(r.Context(), auth.VerifyCodeRequest{
		Channel:     channel,
		Destination: body.Destination,
		Code:        strings.TrimSpace(body.Code),
		RequestIP:   requestIP(r),
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidDestination), errors.Is(err, auth.ErrInvalidChannel):
			writeValidationError(w, r, "That destination is not a valid email address or phone number.")
		case errors.Is(err, auth.ErrTooManyAttempts):
			writeRateLimited(w, r, auth.DefaultOTPTTL, "Too many attempts. Request a new code.")
		case errors.Is(err, auth.ErrCodeNotFound), errors.Is(err, auth.ErrCodeMismatch), errors.Is(err, auth.ErrCodeExpired):
			writeUnauthorized(w, r, "That code is not valid or has expired.")
		default:
			s.writeInternalError(w, r, err)
		}
		return
	}

	signIn, err := s.accounts.EnsureAccountForContact(r.Context(), contact)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	s.issueSession(w, r, signIn, body.DeviceID, http.StatusOK)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
	WorkspaceID  string `json:"workspace_id"`
	DeviceID     string `json:"device_id"`
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	body := refreshRequest{}
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	presented := s.presentedRefreshToken(r, body.RefreshToken)
	if presented == "" {
		writeUnauthorized(w, r, "A refresh token is required.")
		return
	}

	var requestedWorkspace uuid.UUID
	if raw := strings.TrimSpace(body.WorkspaceID); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			writeValidationError(w, r, "workspace_id is not a valid identifier.")
			return
		}
		requestedWorkspace = parsed
	}

	rotated, err := s.refresh.Rotate(r.Context(), presented, body.DeviceID)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrRefreshReused):
			hlog.FromRequest(r).Warn().Msg("refresh token reuse detected, session family revoked")
			http.SetCookie(w, s.expiredRefreshCookie())
			writeUnauthorized(w, r, "That session has been ended. Sign in again.")
		case errors.Is(err, auth.ErrRefreshNotFound), errors.Is(err, auth.ErrRefreshRevoked), errors.Is(err, auth.ErrRefreshExpired):
			http.SetCookie(w, s.expiredRefreshCookie())
			writeUnauthorized(w, r, "That refresh token is not valid.")
		default:
			s.writeInternalError(w, r, err)
		}
		return
	}

	user, err := s.accounts.GetUser(r.Context(), rotated.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	workspace, role, err := s.resolveWorkspace(r.Context(), user.ID, requestedWorkspace)
	if err != nil {
		if errors.Is(err, auth.ErrNotMember) {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
			return
		}
		s.writeServiceError(w, r, err)
		return
	}

	access, err := s.tokens.Issue(user.ID, workspace.ID, role)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	http.SetCookie(w, s.refreshCookie(rotated.Token, rotated.ExpiresAt))
	httpx.WriteJSON(w, r, http.StatusOK, sessionView{
		AccessToken:      access.Token,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.tokens.TTL().Seconds()),
		ExpiresAt:        access.ExpiresAt,
		RefreshToken:     rotated.Token,
		RefreshExpiresAt: rotated.ExpiresAt,
		User:             newUserView(user),
		Workspace:        newWorkspaceView(workspace, role),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	body := refreshRequest{}
	if err := decodeOptionalJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	presented := s.presentedRefreshToken(r, body.RefreshToken)
	if presented != "" {
		if err := s.refresh.Revoke(r.Context(), presented); err != nil && !errors.Is(err, auth.ErrRefreshNotFound) {
			s.writeInternalError(w, r, err)
			return
		}
	}

	http.SetCookie(w, s.expiredRefreshCookie())
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) presentedRefreshToken(r *http.Request, fromBody string) string {
	if presented := strings.TrimSpace(fromBody); presented != "" {
		return presented
	}
	cookie, err := r.Cookie(RefreshCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, "Sign in to continue.")
		return
	}

	user, err := s.accounts.GetUser(r.Context(), identity.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	memberships, err := s.accounts.ListWorkspaces(r.Context(), identity.UserID)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	workspaces := make([]workspaceView, 0, len(memberships))
	for _, membership := range memberships {
		workspaces = append(workspaces, newWorkspaceView(membership.Workspace, membership.Role))
	}

	httpx.WriteJSON(w, r, http.StatusOK, meView{User: newUserView(user), Workspaces: workspaces})
}

func (s *Server) issueSession(w http.ResponseWriter, r *http.Request, signIn accounts.SignIn, deviceID string, status int) {
	access, err := s.tokens.Issue(signIn.User.ID, signIn.Workspace.ID, signIn.Role)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	refresh, err := s.refresh.Issue(r.Context(), signIn.User.ID, deviceID)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	http.SetCookie(w, s.refreshCookie(refresh.Token, refresh.ExpiresAt))
	httpx.WriteJSON(w, r, status, sessionView{
		AccessToken:      access.Token,
		TokenType:        "Bearer",
		ExpiresIn:        int64(s.tokens.TTL().Seconds()),
		ExpiresAt:        access.ExpiresAt,
		RefreshToken:     refresh.Token,
		RefreshExpiresAt: refresh.ExpiresAt,
		User:             newUserView(signIn.User),
		Workspace:        newWorkspaceView(signIn.Workspace, signIn.Role),
	})
}

func (s *Server) resolveWorkspace(ctx context.Context, userID, requested uuid.UUID) (accounts.Workspace, auth.Role, error) {
	memberships, err := s.accounts.ListWorkspaces(ctx, userID)
	if err != nil {
		return accounts.Workspace{}, "", err
	}
	if len(memberships) == 0 {
		return accounts.Workspace{}, "", accounts.ErrWorkspaceNotFound
	}
	if requested == uuid.Nil {
		return memberships[0].Workspace, memberships[0].Role, nil
	}
	for _, membership := range memberships {
		if membership.Workspace.ID == requested {
			return membership.Workspace, membership.Role, nil
		}
	}
	return accounts.Workspace{}, "", auth.ErrNotMember
}
