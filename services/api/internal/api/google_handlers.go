package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const (
	googleErrorPath       = "/sign-in?error=google"
	googleStateCookieName = "afterword_google_state"
	googleStateCookiePath = "/v1/auth/google"
)

func (s *Server) handleGoogleStart(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured, "Google sign-in is not available yet.")
		return
	}

	started, err := s.google.Start(r.Context(), r.URL.Query().Get("redirect_to"))
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	http.SetCookie(w, s.googleStateCookie(started.Nonce, started.ExpiresAt))
	http.Redirect(w, r, started.AuthorizationURL, http.StatusFound)
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured, "Google sign-in is not available yet.")
		return
	}

	var presentedNonce string
	if cookie, err := r.Cookie(googleStateCookieName); err == nil {
		presentedNonce = cookie.Value
	}
	http.SetCookie(w, s.expiredGoogleStateCookie())

	query := r.URL.Query()
	if provider := query.Get("error"); provider != "" {
		hlog.FromRequest(r).Warn().Str("provider_error", provider).Msg("google returned an error")
		s.redirectToApp(w, r, googleErrorPath)
		return
	}

	completed, err := s.google.Complete(r.Context(), query.Get("state"), query.Get("code"), presentedNonce)
	if err != nil {
		if errors.Is(err, auth.ErrStateNotFound) || errors.Is(err, auth.ErrStateExpired) ||
			errors.Is(err, auth.ErrStateNonceMismatch) || errors.Is(err, auth.ErrEmailNotVerified) {
			hlog.FromRequest(r).Warn().Err(err).Msg("google sign-in was refused")
		} else {
			hlog.FromRequest(r).Error().Err(err).Msg("google sign-in failed")
		}
		s.redirectToApp(w, r, googleErrorPath)
		return
	}

	signIn, err := s.accounts.EnsureAccountForGoogle(r.Context(), completed.Profile)
	if err != nil {
		hlog.FromRequest(r).Error().Err(err).Msg("google sign-in could not create the account")
		s.redirectToApp(w, r, googleErrorPath)
		return
	}

	refresh, err := s.refresh.Issue(r.Context(), signIn.User.ID, "web")
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	http.SetCookie(w, s.refreshCookie(refresh.Token, refresh.ExpiresAt))

	destination := auth.SafeRedirectPath(completed.RedirectTo)
	if destination == "" {
		destination = "/"
	}
	s.redirectToApp(w, r, destination)
}

func (s *Server) redirectToApp(w http.ResponseWriter, r *http.Request, path string) {
	http.Redirect(w, r, s.appBaseURL+path, http.StatusFound)
}

func (s *Server) googleStateCookie(nonce string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     googleStateCookieName,
		Value:    nonce,
		Path:     googleStateCookiePath,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSafe,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) expiredGoogleStateCookie() *http.Cookie {
	return &http.Cookie{
		Name:     googleStateCookieName,
		Value:    "",
		Path:     googleStateCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSafe,
		SameSite: http.SameSiteLaxMode,
	}
}
