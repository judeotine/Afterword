package accounts

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

var (
	ErrUserNotFound        = errors.New("accounts: user not found")
	ErrWorkspaceNotFound   = errors.New("accounts: workspace not found")
	ErrMemberNotFound      = errors.New("accounts: member not found")
	ErrInviteNotFound      = errors.New("accounts: invite not found")
	ErrInviteExpired       = errors.New("accounts: invite has expired")
	ErrInviteAlreadyUsed   = errors.New("accounts: invite has already been used")
	ErrInviteEmailMismatch = errors.New("accounts: invite was issued to another address")
	ErrInviteExists        = errors.New("accounts: an invite is already outstanding for that address")
	ErrAlreadyMember       = errors.New("accounts: user is already a member of that workspace")
	ErrLastOwner           = errors.New("accounts: a workspace must keep at least one owner")
	ErrOwnerRoleRequired   = errors.New("accounts: only an owner can manage owners")
	ErrInvalidName         = errors.New("accounts: name is required")
	ErrInvalidSlug         = errors.New("accounts: slug is not valid")
	ErrSlugTaken           = errors.New("accounts: slug is already taken")
	ErrInvalidEmail        = errors.New("accounts: email address is not valid")
	ErrSelfDemotion        = errors.New("accounts: you cannot change your own role")
)

type User struct {
	ID        uuid.UUID
	Email     string
	Phone     string
	Name      string
	CreatedAt time.Time
}

type Workspace struct {
	ID            uuid.UUID
	Name          string
	Slug          string
	BotName       string
	RetentionDays int32
	CreatedAt     time.Time
}

type WorkspaceMembership struct {
	Workspace Workspace
	Role      auth.Role
}

type Member struct {
	WorkspaceID uuid.UUID
	UserID      uuid.UUID
	Email       string
	Phone       string
	Name        string
	Role        auth.Role
	JoinedAt    time.Time
}

type Invite struct {
	ID          uuid.UUID
	WorkspaceID uuid.UUID
	Email       string
	Role        auth.Role
	ExpiresAt   time.Time
	AcceptedAt  *time.Time
	CreatedAt   time.Time
}

func userFromRow(row sqlcgen.User) User {
	return User{
		ID:        row.ID,
		Email:     stringValue(row.Email),
		Phone:     stringValue(row.Phone),
		Name:      row.Name,
		CreatedAt: moment(row.CreatedAt),
	}
}

func workspaceFromRow(row sqlcgen.Workspace) Workspace {
	return Workspace{
		ID:            row.ID,
		Name:          row.Name,
		Slug:          row.Slug,
		BotName:       row.BotName,
		RetentionDays: row.RetentionDays,
		CreatedAt:     moment(row.CreatedAt),
	}
}

func inviteFromRow(row sqlcgen.WorkspaceInvite) Invite {
	return Invite{
		ID:          row.ID,
		WorkspaceID: row.WorkspaceID,
		Email:       row.Email,
		Role:        auth.Role(row.Role),
		ExpiresAt:   moment(row.ExpiresAt),
		AcceptedAt:  optionalMoment(row.AcceptedAt),
		CreatedAt:   moment(row.CreatedAt),
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func optionalString(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func timestamp(at time.Time) pgtype.Timestamptz {
	if at.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: at.UTC(), Valid: true}
}

func moment(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time.UTC()
}

func optionalMoment(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	at := value.Time.UTC()
	return &at
}
