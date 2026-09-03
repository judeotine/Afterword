package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/hlog"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/httpx"
)

type createWorkspaceRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func (s *Server) handleCreateWorkspace(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, "Sign in to continue.")
		return
	}

	var body createWorkspaceRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	membership, err := s.accounts.CreateWorkspace(r.Context(), identity.UserID, body.Name, body.Slug)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, r, http.StatusCreated, newWorkspaceView(membership.Workspace, membership.Role))
}

type workspaceListView struct {
	Workspaces []workspaceView `json:"workspaces"`
}

func (s *Server) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, "Sign in to continue.")
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
	httpx.WriteJSON(w, r, http.StatusOK, workspaceListView{Workspaces: workspaces})
}

func (s *Server) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	workspace, err := s.accounts.GetWorkspace(r.Context(), membership.WorkspaceID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}
	httpx.WriteJSON(w, r, http.StatusOK, newWorkspaceView(workspace, membership.Role))
}

type memberListView struct {
	Members []memberView `json:"members"`
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	members, err := s.accounts.ListMembers(r.Context(), membership.WorkspaceID)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	views := make([]memberView, 0, len(members))
	for _, member := range members {
		views = append(views, newMemberView(member))
	}
	httpx.WriteJSON(w, r, http.StatusOK, memberListView{Members: views})
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleUpdateMember(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	targetUserID, ok := pathUUID(r, userParam)
	if !ok {
		writeValidationError(w, r, "The member id is not a valid identifier.")
		return
	}

	var body updateMemberRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	role, err := auth.ParseRole(strings.TrimSpace(body.Role))
	if err != nil {
		writeValidationError(w, r, "role must be owner, admin or member.")
		return
	}

	member, err := s.accounts.UpdateMemberRole(r.Context(), membership, targetUserID, role)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, r, http.StatusOK, newMemberView(member))
}

func (s *Server) handleRemoveMember(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	targetUserID, ok := pathUUID(r, userParam)
	if !ok {
		writeValidationError(w, r, "The member id is not a valid identifier.")
		return
	}

	if err := s.accounts.RemoveMember(r.Context(), membership, targetUserID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	var body createInviteRequest
	if err := decodeJSON(w, r, &body); err != nil {
		writeInvalidBody(w, r)
		return
	}

	role := auth.RoleMember
	if trimmed := strings.TrimSpace(body.Role); trimmed != "" {
		parsed, err := auth.ParseRole(trimmed)
		if err != nil {
			writeValidationError(w, r, "role must be owner, admin or member.")
			return
		}
		role = parsed
	}

	created, err := s.accounts.CreateInvite(r.Context(), accounts.CreateInviteParams{
		Actor: membership,
		Email: body.Email,
		Role:  role,
	})
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	workspace, err := s.accounts.GetWorkspace(r.Context(), membership.WorkspaceID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	view := newInviteView(created.Invite)
	view.Token = created.Token
	view.AcceptURL = s.inviteURL(created.Token)

	s.sendInviteEmail(r, workspace, created)

	httpx.WriteJSON(w, r, http.StatusCreated, view)
}

type inviteListView struct {
	Invites []inviteView `json:"invites"`
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	invites, err := s.accounts.ListInvites(r.Context(), membership.WorkspaceID, 0)
	if err != nil {
		s.writeInternalError(w, r, err)
		return
	}

	views := make([]inviteView, 0, len(invites))
	for _, invite := range invites {
		views = append(views, newInviteView(invite))
	}
	httpx.WriteJSON(w, r, http.StatusOK, inviteListView{Invites: views})
}

func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	membership, ok := auth.MembershipFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.CodeForbidden, "You do not have access to that workspace.")
		return
	}

	inviteID, ok := pathUUID(r, inviteParam)
	if !ok {
		writeValidationError(w, r, "The invite id is not a valid identifier.")
		return
	}

	if err := s.accounts.RevokeInvite(r.Context(), membership.WorkspaceID, inviteID); err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type acceptInviteResponse struct {
	Workspace workspaceView `json:"workspace"`
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		writeUnauthorized(w, r, "Sign in to continue.")
		return
	}

	token := strings.TrimSpace(chi.URLParam(r, inviteTokenParam))
	if token == "" {
		httpx.WriteError(w, r, http.StatusNotFound, httpx.CodeNotFound, "That invite does not exist.")
		return
	}

	user, err := s.accounts.GetUser(r.Context(), identity.UserID)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	accepted, err := s.accounts.AcceptInvite(r.Context(), token, user)
	if err != nil {
		s.writeServiceError(w, r, err)
		return
	}

	httpx.WriteJSON(w, r, http.StatusOK, acceptInviteResponse{
		Workspace: newWorkspaceView(accepted.Workspace, accepted.Role),
	})
}

func (s *Server) inviteURL(token string) string {
	if s.appBaseURL == "" {
		return ""
	}
	return s.appBaseURL + "/invites/" + token
}

func (s *Server) sendInviteEmail(r *http.Request, workspace accounts.Workspace, created accounts.CreatedInvite) {
	if s.email == nil {
		return
	}
	message := auth.EmailMessage{
		To:      created.Invite.Email,
		Subject: "You have been invited to " + workspace.Name + " on " + s.productName,
		Text: "You have been invited to join the " + workspace.Name + " workspace on " + s.productName +
			" as " + string(created.Invite.Role) + ".\n\nAccept the invite: " + s.inviteURL(created.Token) +
			"\n\nThe invite expires on " + created.Invite.ExpiresAt.Format("2 January 2006") + ".",
	}
	if err := s.email.SendEmail(r.Context(), message); err != nil {
		hlog.FromRequest(r).Error().Err(err).Msg("invite email could not be sent")
	}
}
