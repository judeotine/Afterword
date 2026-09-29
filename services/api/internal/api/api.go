package api

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/billing"
	"github.com/judeotine/afterword/services/api/internal/meetings"
)

const (
	RefreshCookieName = "afterword_refresh"
	refreshCookiePath = "/v1/auth"
	workspaceParam    = "workspaceID"
	userParam         = "userID"
	inviteParam       = "inviteID"
	inviteTokenParam  = "token"
	meetingParam      = "meetingID"
	folderParam       = "folderID"
	shareTokenParam   = "shareToken"
)

type ServerOptions struct {
	Accounts    *accounts.Service
	OTP         *auth.OTPService
	Tokens      *auth.TokenIssuer
	Refresh     *auth.RefreshManager
	Middleware  *auth.Middleware
	Meetings    *meetings.Service
	Google      *auth.GoogleAuthenticator
	Email       auth.EmailSender
	AppBaseURL  string
	ProductName string
	Clock       func() time.Time
}

type Server struct {
	accounts    *accounts.Service
	otp         *auth.OTPService
	tokens      *auth.TokenIssuer
	refresh     *auth.RefreshManager
	middleware  *auth.Middleware
	meetings    *meetings.Service
	google      *auth.GoogleAuthenticator
	email       auth.EmailSender
	appBaseURL  string
	productName string
	cookieSafe  bool
	clock       func() time.Time
}

func NewServer(options ServerOptions) (*Server, error) {
	switch {
	case options.Accounts == nil:
		return nil, errors.New("api: an accounts service is required")
	case options.OTP == nil:
		return nil, errors.New("api: an otp service is required")
	case options.Tokens == nil:
		return nil, errors.New("api: a token issuer is required")
	case options.Refresh == nil:
		return nil, errors.New("api: a refresh manager is required")
	case options.Middleware == nil:
		return nil, errors.New("api: auth middleware is required")
	}

	server := &Server{
		accounts:    options.Accounts,
		otp:         options.OTP,
		tokens:      options.Tokens,
		refresh:     options.Refresh,
		middleware:  options.Middleware,
		meetings:    options.Meetings,
		google:      options.Google,
		email:       options.Email,
		appBaseURL:  strings.TrimRight(strings.TrimSpace(options.AppBaseURL), "/"),
		productName: options.ProductName,
		clock:       options.Clock,
	}
	if server.productName == "" {
		server.productName = "Afterword"
	}
	if server.clock == nil {
		server.clock = time.Now
	}
	if parsed, err := url.Parse(server.appBaseURL); err == nil && parsed.Scheme == "https" {
		server.cookieSafe = true
	}
	return server, nil
}

func (s *Server) topUpURL() string {
	if s.appBaseURL == "" {
		return ""
	}
	return s.appBaseURL + billing.TopUpPath
}

func (s *Server) Routes(router chi.Router) {
	router.Route("/v1", func(r chi.Router) {
		r.Route("/auth", func(r chi.Router) {
			r.Post("/otp/send", s.handleSendCode)
			r.Post("/otp/verify", s.handleVerifyCode)
			r.Get("/google/start", s.handleGoogleStart)
			r.Get("/google/callback", s.handleGoogleCallback)
			r.Post("/refresh", s.handleRefresh)
			r.Post("/logout", s.handleLogout)
		})

		r.Route("/shared/{"+shareTokenParam+"}", func(r chi.Router) {
			r.Get("/", s.handleSharedMeeting)
			r.Get("/segments", s.handleSharedSegments)
		})

		r.Group(func(r chi.Router) {
			r.Use(s.middleware.RequireAuth)

			r.Get("/me", s.handleMe)
			r.Post("/workspaces", s.handleCreateWorkspace)
			r.Get("/workspaces", s.handleListWorkspaces)
			r.Post("/invites/{"+inviteTokenParam+"}/accept", s.handleAcceptInvite)

			r.Route("/workspaces/{"+workspaceParam+"}", func(r chi.Router) {
				r.Use(s.middleware.RequireWorkspace)

				r.Get("/", s.handleGetWorkspace)
				r.Get("/members", s.handleListMembers)
				r.Delete("/members/{"+userParam+"}", s.handleRemoveMember)

				r.Group(func(r chi.Router) {
					r.Use(s.middleware.RequireRole(auth.RoleAdmin))
					r.Post("/invites", s.handleCreateInvite)
					r.Get("/invites", s.handleListInvites)
					r.Delete("/invites/{"+inviteParam+"}", s.handleRevokeInvite)
					r.Patch("/members/{"+userParam+"}", s.handleUpdateMember)
				})
			})

			r.Group(func(r chi.Router) {
				r.Use(s.middleware.RequireWorkspace)

				r.Post("/meetings", s.handleCreateMeeting)
				r.Get("/meetings", s.handleListMeetings)

				r.Route("/meetings/{"+meetingParam+"}", func(r chi.Router) {
					r.Get("/", s.handleGetMeeting)
					r.Patch("/", s.handleUpdateMeeting)
					r.Delete("/", s.handleDeleteMeeting)
					r.Post("/finalize", s.handleFinalizeMeeting)
					r.Post("/upload-urls", s.handleUploadTargets)
					r.Put("/segments", s.handleReplaceSegments)
					r.Get("/segments", s.handleListSegments)
					r.Post("/share", s.handleCreateShareLink)
					r.Get("/share", s.handleListShareLinks)
					r.Delete("/share", s.handleRevokeShareLinks)
				})

				r.Post("/folders", s.handleCreateFolder)
				r.Get("/folders", s.handleListFolders)
				r.Patch("/folders/{"+folderParam+"}", s.handleUpdateFolder)
				r.Delete("/folders/{"+folderParam+"}", s.handleDeleteFolder)

				r.Get("/search", s.handleSearch)
			})
		})
	})
}

func (s *Server) refreshCookie(token string, expiresAt time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		Secure:   s.cookieSafe,
		SameSite: http.SameSiteLaxMode,
	}
}

func (s *Server) expiredRefreshCookie() *http.Cookie {
	return &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookieSafe,
		SameSite: http.SameSiteLaxMode,
	}
}
