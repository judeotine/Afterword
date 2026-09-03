package api

import (
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/accounts"
	"github.com/judeotine/afterword/services/api/internal/auth"
)

type userView struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email,omitempty"`
	Phone     string    `json:"phone,omitempty"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type workspaceView struct {
	ID            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Slug          string    `json:"slug"`
	BotName       string    `json:"bot_name"`
	RetentionDays int32     `json:"retention_days"`
	Role          auth.Role `json:"role,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type memberView struct {
	UserID   uuid.UUID `json:"user_id"`
	Email    string    `json:"email,omitempty"`
	Phone    string    `json:"phone,omitempty"`
	Name     string    `json:"name"`
	Role     auth.Role `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type inviteView struct {
	ID          uuid.UUID  `json:"id"`
	WorkspaceID uuid.UUID  `json:"workspace_id"`
	Email       string     `json:"email"`
	Role        auth.Role  `json:"role"`
	ExpiresAt   time.Time  `json:"expires_at"`
	AcceptedAt  *time.Time `json:"accepted_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	Token       string     `json:"token,omitempty"`
	AcceptURL   string     `json:"accept_url,omitempty"`
}

type sessionView struct {
	AccessToken      string        `json:"access_token"`
	TokenType        string        `json:"token_type"`
	ExpiresIn        int64         `json:"expires_in"`
	ExpiresAt        time.Time     `json:"expires_at"`
	RefreshToken     string        `json:"refresh_token"`
	RefreshExpiresAt time.Time     `json:"refresh_expires_at"`
	User             userView      `json:"user"`
	Workspace        workspaceView `json:"workspace"`
}

type meView struct {
	User       userView        `json:"user"`
	Workspaces []workspaceView `json:"workspaces"`
}

func newUserView(user accounts.User) userView {
	return userView{
		ID:        user.ID,
		Email:     user.Email,
		Phone:     user.Phone,
		Name:      user.Name,
		CreatedAt: user.CreatedAt,
	}
}

func newWorkspaceView(workspace accounts.Workspace, role auth.Role) workspaceView {
	return workspaceView{
		ID:            workspace.ID,
		Name:          workspace.Name,
		Slug:          workspace.Slug,
		BotName:       workspace.BotName,
		RetentionDays: workspace.RetentionDays,
		Role:          role,
		CreatedAt:     workspace.CreatedAt,
	}
}

func newMemberView(member accounts.Member) memberView {
	return memberView{
		UserID:   member.UserID,
		Email:    member.Email,
		Phone:    member.Phone,
		Name:     member.Name,
		Role:     member.Role,
		JoinedAt: member.JoinedAt,
	}
}

func newInviteView(invite accounts.Invite) inviteView {
	return inviteView{
		ID:          invite.ID,
		WorkspaceID: invite.WorkspaceID,
		Email:       invite.Email,
		Role:        invite.Role,
		ExpiresAt:   invite.ExpiresAt,
		AcceptedAt:  invite.AcceptedAt,
		CreatedAt:   invite.CreatedAt,
	}
}
