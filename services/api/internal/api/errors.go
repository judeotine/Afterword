package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

const (
	codeAlreadyMember      = "already_member"
	codeSlugTaken          = "slug_taken"
	codeInviteExists       = "invite_exists"
	codeInviteExpired      = "invite_expired"
	codeInviteUsed         = "invite_used"
	codeInviteMismatch     = "invite_email_mismatch"
	codeLastOwner          = "last_owner"
	codeOwnerRoleRequired  = "owner_role_required"
	codeNotConfigured      = "not_configured"
	genericInternalMessage = "Something went wrong."
)

type statusError struct {
	status  int
	code    string
	message string
}

var serviceErrors = []struct {
	target error
	result statusError
}{
	{accounts.ErrInvalidName, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "A workspace name is required."}},
	{accounts.ErrInvalidSlug, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "A slug may only contain lowercase letters, numbers and dashes."}},
	{accounts.ErrInvalidEmail, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That email address is not valid."}},
	{auth.ErrInvalidRole, statusError{http.StatusBadRequest, httpx.CodeValidationFailed, "That role is not recognised."}},
	{accounts.ErrSlugTaken, statusError{http.StatusConflict, codeSlugTaken, "That workspace address is already taken."}},
	{accounts.ErrAlreadyMember, statusError{http.StatusConflict, codeAlreadyMember, "That person is already a member of this workspace."}},
	{accounts.ErrInviteExists, statusError{http.StatusConflict, codeInviteExists, "An invite is already outstanding for that address."}},
	{accounts.ErrInviteAlreadyUsed, statusError{http.StatusConflict, codeInviteUsed, "That invite has already been used."}},
	{accounts.ErrInviteExpired, statusError{http.StatusGone, codeInviteExpired, "That invite has expired."}},
	{accounts.ErrInviteEmailMismatch, statusError{http.StatusForbidden, codeInviteMismatch, "That invite was issued to a different email address."}},
	{accounts.ErrInviteNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That invite does not exist."}},
	{accounts.ErrMemberNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That member does not exist."}},
	{accounts.ErrUserNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That user does not exist."}},
	{accounts.ErrWorkspaceNotFound, statusError{http.StatusNotFound, httpx.CodeNotFound, "That workspace does not exist."}},
	{accounts.ErrLastOwner, statusError{http.StatusForbidden, codeLastOwner, "A workspace must keep at least one owner."}},
	{accounts.ErrOwnerRoleRequired, statusError{http.StatusForbidden, codeOwnerRoleRequired, "Only an owner can manage owners."}},
	{accounts.ErrSelfDemotion, statusError{http.StatusForbidden, httpx.CodeForbidden, "You cannot change your own role."}},
	{accounts.ErrNotPermitted, statusError{http.StatusForbidden, httpx.CodeForbidden, "Your role does not allow that action."}},
}

func (s *Server) writeServiceError(w http.ResponseWriter, r *http.Request, err error) {
	for _, candidate := range serviceErrors {
		if errors.Is(err, candidate.target) {
			httpx.WriteError(w, r, candidate.result.status, candidate.result.code, candidate.result.message)
			return
		}
	}
	s.writeInternalError(w, r, err)
}

func (s *Server) writeInternalError(w http.ResponseWriter, r *http.Request, err error) {
	hlog.FromRequest(r).Error().Err(err).Msg("request failed")
	httpx.WriteError(w, r, http.StatusInternalServerError, httpx.CodeInternalError, genericInternalMessage)
}

func writeRateLimited(w http.ResponseWriter, r *http.Request, retryAfter time.Duration, message string) {
	if retryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
	}
	httpx.WriteError(w, r, http.StatusTooManyRequests, httpx.CodeRateLimited, message)
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, message string) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="afterword"`)
	httpx.WriteError(w, r, http.StatusUnauthorized, httpx.CodeUnauthorized, message)
}
