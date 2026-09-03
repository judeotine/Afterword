package api

import (
	"errors"
	"net/http"

	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const googleErrorPath = "/sign-in?error=google"

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

	http.Redirect(w, r, started.AuthorizationURL, http.StatusFound)
}

func (s *Server) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if s.google == nil {
		httpx.WriteError(w, r, http.StatusServiceUnavailable, codeNotConfigured, "Google sign-in is not available yet.")
		return
	}

	query := r.URL.Query()
	if provider := query.Get("error"); provider != "" {
		hlog.FromRequest(r).Warn().Str("provider_error", provider).Msg("google returned an error")
		s.redirectToApp(w, r, googleErrorPath)
		return
	}

	completed, err := s.google.Complete(r.Context(), query.Get("state"), query.Get("code"))
	if err != nil {
		if errors.Is(err, auth.ErrStateNotFound) || errors.Is(err, auth.ErrStateExpired) || errors.Is(err, auth.ErrEmailNotVerified) {
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
