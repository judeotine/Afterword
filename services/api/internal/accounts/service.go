package accounts

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

const (
	DefaultInviteTTL      = 7 * 24 * time.Hour
	InviteTokenBytes      = 32
	defaultBotName        = "Afterword Notetaker"
	defaultRetentionDays  = 365
	fallbackWorkspaceName = "Personal workspace"
	fallbackSlug          = "workspace"
	slugAttempts          = 5
	accountAttempts       = 3
)

var ErrNotPermitted = errors.New("accounts: that action is not allowed for your role")

type Service struct {
	pool      *pgxpool.Pool
	queries   *sqlcgen.Queries
	inviteTTL time.Duration
	clock     func() time.Time
}

type ServiceOption func(*Service)

func WithInviteTTL(ttl time.Duration) ServiceOption {
	return func(s *Service) {
		if ttl > 0 {
			s.inviteTTL = ttl
		}
	}
}

func WithClock(clock func() time.Time) ServiceOption {
	return func(s *Service) {
		if clock != nil {
			s.clock = clock
		}
	}
}

func NewService(pool *pgxpool.Pool, opts ...ServiceOption) (*Service, error) {
	if pool == nil {
		return nil, errors.New("accounts: a database pool is required")
	}
	service := &Service{
		pool:      pool,
		queries:   sqlcgen.New(pool),
		inviteTTL: DefaultInviteTTL,
		clock:     time.Now,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service, nil
}

type EnsureAccountParams struct {
	Email string
	Phone string
	Name  string
}

type SignIn struct {
	User      User
	Workspace Workspace
	Role      auth.Role
	Created   bool
}

func (s *Service) EnsureAccount(ctx context.Context, params EnsureAccountParams) (SignIn, error) {
	email := strings.ToLower(strings.TrimSpace(params.Email))
	phone := strings.TrimSpace(params.Phone)
	if email == "" && phone == "" {
		return SignIn{}, ErrInvalidEmail
	}

	var result SignIn
	var lastErr error
	for attempt := 0; attempt < accountAttempts; attempt++ {
		lastErr = s.inTx(ctx, func(q *sqlcgen.Queries) error {
			signIn, err := s.ensureAccountTx(ctx, q, email, phone, strings.TrimSpace(params.Name))
			if err != nil {
				return err
			}
			result = signIn
			return nil
		})
		if lastErr == nil {
			return result, nil
		}
		if !isUniqueViolation(lastErr) {
			return SignIn{}, lastErr
		}
	}
	return SignIn{}, lastErr
}

func (s *Service) ensureAccountTx(ctx context.Context, q *sqlcgen.Queries, email, phone, name string) (SignIn, error) {
	row, found, err := lookupUser(ctx, q, email, phone)
	if err != nil {
		return SignIn{}, err
	}

	created := false
	if !found {
		row, err = q.CreateUser(ctx, sqlcgen.CreateUserParams{
			Email: optionalString(email),
			Phone: optionalString(phone),
			Name:  name,
		})
		if err != nil {
			return SignIn{}, fmt.Errorf("create user: %w", err)
		}
		created = true
	} else if name != "" && strings.TrimSpace(row.Name) == "" {
		row, err = q.UpdateUser(ctx, sqlcgen.UpdateUserParams{ID: row.ID, Name: &name})
		if err != nil {
			return SignIn{}, fmt.Errorf("update user name: %w", err)
		}
	}

	user := userFromRow(row)

	workspace, role, err := defaultWorkspace(ctx, q, user.ID)
	if err == nil {
		return SignIn{User: user, Workspace: workspace, Role: role, Created: created}, nil
	}
	if !errors.Is(err, ErrWorkspaceNotFound) {
		return SignIn{}, err
	}

	workspace, err = createWorkspaceTx(ctx, q, personalWorkspaceName(user), "", user.ID)
	if err != nil {
		return SignIn{}, err
	}
	return SignIn{User: user, Workspace: workspace, Role: auth.RoleOwner, Created: created}, nil
}

func (s *Service) EnsureAccountForContact(ctx context.Context, contact auth.VerifiedContact) (SignIn, error) {
	params := EnsureAccountParams{}
	switch contact.Channel {
	case auth.ChannelEmail:
		params.Email = contact.Destination
	case auth.ChannelPhone:
		params.Phone = contact.Destination
	default:
		return SignIn{}, auth.ErrInvalidChannel
	}
	return s.EnsureAccount(ctx, params)
}

func (s *Service) EnsureAccountForGoogle(ctx context.Context, profile auth.GoogleProfile) (SignIn, error) {
	if strings.TrimSpace(profile.Email) == "" {
		return SignIn{}, ErrInvalidEmail
	}
	return s.EnsureAccount(ctx, EnsureAccountParams{Email: profile.Email, Name: profile.Name})
}

func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (User, error) {
	row, err := s.queries.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrUserNotFound
		}
		return User{}, fmt.Errorf("read user: %w", err)
	}
	return userFromRow(row), nil
}

func (s *Service) LoadMembership(ctx context.Context, workspaceID, userID uuid.UUID) (auth.Membership, error) {
	row, err := s.queries.GetMembership(ctx, sqlcgen.GetMembershipParams{WorkspaceID: workspaceID, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.Membership{}, auth.ErrNotMember
		}
		return auth.Membership{}, fmt.Errorf("read membership: %w", err)
	}
	return auth.Membership{WorkspaceID: row.WorkspaceID, UserID: row.UserID, Role: auth.Role(row.Role)}, nil
}

func (s *Service) GetWorkspace(ctx context.Context, workspaceID uuid.UUID) (Workspace, error) {
	row, err := s.queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Workspace{}, ErrWorkspaceNotFound
		}
		return Workspace{}, fmt.Errorf("read workspace: %w", err)
	}
	return workspaceFromRow(row), nil
}

func (s *Service) ListWorkspaces(ctx context.Context, userID uuid.UUID) ([]WorkspaceMembership, error) {
	rows, err := s.queries.ListWorkspacesWithRoleByUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	memberships := make([]WorkspaceMembership, 0, len(rows))
	for _, row := range rows {
		memberships = append(memberships, WorkspaceMembership{
			Workspace: Workspace{
				ID:            row.ID,
				Name:          row.Name,
				Slug:          row.Slug,
				BotName:       row.BotName,
				RetentionDays: row.RetentionDays,
				CreatedAt:     moment(row.CreatedAt),
			},
			Role: auth.Role(row.MembershipRole),
		})
	}
	return memberships, nil
}

func (s *Service) CreateWorkspace(ctx context.Context, userID uuid.UUID, name, slug string) (WorkspaceMembership, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" || len([]rune(trimmedName)) > 120 {
		return WorkspaceMembership{}, ErrInvalidName
	}
	trimmedSlug := strings.ToLower(strings.TrimSpace(slug))
	if trimmedSlug != "" && !ValidSlug(trimmedSlug) {
		return WorkspaceMembership{}, ErrInvalidSlug
	}

	var workspace Workspace
	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		created, err := createWorkspaceTx(ctx, q, trimmedName, trimmedSlug, userID)
		if err != nil {
			return err
		}
		workspace = created
		return nil
	})
	if err != nil {
		if isUniqueViolation(err) {
			return WorkspaceMembership{}, ErrSlugTaken
		}
		return WorkspaceMembership{}, err
	}
	return WorkspaceMembership{Workspace: workspace, Role: auth.RoleOwner}, nil
}

func (s *Service) ListMembers(ctx context.Context, workspaceID uuid.UUID) ([]Member, error) {
	rows, err := s.queries.ListWorkspaceMembersWithUsers(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	members := make([]Member, 0, len(rows))
	for _, row := range rows {
		members = append(members, Member{
			WorkspaceID: row.WorkspaceID,
			UserID:      row.UserID,
			Email:       stringValue(row.Email),
			Phone:       stringValue(row.Phone),
			Name:        row.Name,
			Role:        auth.Role(row.Role),
			JoinedAt:    moment(row.CreatedAt),
		})
	}
	return members, nil
}

func (s *Service) UpdateMemberRole(ctx context.Context, actor auth.Membership, targetUserID uuid.UUID, role auth.Role) (Member, error) {
	if !role.Valid() {
		return Member{}, auth.ErrInvalidRole
	}
	if actor.UserID == targetUserID {
		return Member{}, ErrSelfDemotion
	}

	var member Member
	err := s.inTx(ctx, func(q *sqlcgen.Queries) error {
		target, err := q.GetMembership(ctx, sqlcgen.GetMembershipParams{
			WorkspaceID: actor.WorkspaceID,
			UserID:      targetUserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrMemberNotFound
			}
			return fmt.Errorf("read membership: %w", err)
		}

		targetRole := auth.Role(target.Role)
		if (targetRole == auth.RoleOwner || role == auth.RoleOwner) && actor.Role != auth.RoleOwner {
			return ErrOwnerRoleRequired
		}
		if targetRole == auth.RoleOwner && role != auth.RoleOwner {
			owners, err := q.CountWorkspaceOwners(ctx, actor.WorkspaceID)
			if err != nil {
				return fmt.Errorf("count owners: %w", err)
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}

		updated, err := q.UpdateMembership(ctx, sqlcgen.UpdateMembershipParams{
			Role:        string(role),
			WorkspaceID: actor.WorkspaceID,
			UserID:      targetUserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrMemberNotFound
			}
			return fmt.Errorf("update membership: %w", err)
		}

		userRow, err := q.GetUser(ctx, updated.UserID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUserNotFound
			}
			return fmt.Errorf("read user: %w", err)
		}
		user := userFromRow(userRow)

		member = Member{
			WorkspaceID: updated.WorkspaceID,
			UserID:      updated.UserID,
			Email:       user.Email,
			Phone:       user.Phone,
			Name:        user.Name,
			Role:        auth.Role(updated.Role),
			JoinedAt:    moment(updated.CreatedAt),
		}
		return nil
	})
	if err != nil {
		return Member{}, err
	}
	return member, nil
}

func (s *Service) RemoveMember(ctx context.Context, actor auth.Membership, targetUserID uuid.UUID) error {
	return s.inTx(ctx, func(q *sqlcgen.Queries) error {
		target, err := q.GetMembership(ctx, sqlcgen.GetMembershipParams{
			WorkspaceID: actor.WorkspaceID,
			UserID:      targetUserID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrMemberNotFound
			}
			return fmt.Errorf("read membership: %w", err)
		}

		targetRole := auth.Role(target.Role)
		removingSelf := actor.UserID == targetUserID
		if !removingSelf && !actor.Role.AtLeast(auth.RoleAdmin) {
			return ErrNotPermitted
		}
		if !removingSelf && targetRole == auth.RoleOwner && actor.Role != auth.RoleOwner {
			return ErrOwnerRoleRequired
		}
		if targetRole == auth.RoleOwner {
			owners, err := q.CountWorkspaceOwners(ctx, actor.WorkspaceID)
			if err != nil {
				return fmt.Errorf("count owners: %w", err)
			}
			if owners <= 1 {
				return ErrLastOwner
			}
		}

		rows, err := q.DeleteMembership(ctx, sqlcgen.DeleteMembershipParams{
			WorkspaceID: actor.WorkspaceID,
			UserID:      targetUserID,
		})
		if err != nil {
			return fmt.Errorf("delete membership: %w", err)
		}
		if rows == 0 {
			return ErrMemberNotFound
		}
		return nil
	})
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	if err := fn(s.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func lookupUser(ctx context.Context, q *sqlcgen.Queries, email, phone string) (sqlcgen.User, bool, error) {
	if email != "" {
		row, err := q.GetUserByEmail(ctx, email)
		if err == nil {
			return row, true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return sqlcgen.User{}, false, fmt.Errorf("read user by email: %w", err)
		}
		return sqlcgen.User{}, false, nil
	}

	row, err := q.GetUserByPhone(ctx, phone)
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.User{}, false, fmt.Errorf("read user by phone: %w", err)
	}
	return sqlcgen.User{}, false, nil
}

func defaultWorkspace(ctx context.Context, q *sqlcgen.Queries, userID uuid.UUID) (Workspace, auth.Role, error) {
	row, err := q.GetDefaultWorkspaceForUser(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Workspace{}, "", ErrWorkspaceNotFound
		}
		return Workspace{}, "", fmt.Errorf("read default workspace: %w", err)
	}
	return Workspace{
		ID:            row.ID,
		Name:          row.Name,
		Slug:          row.Slug,
		BotName:       row.BotName,
		RetentionDays: row.RetentionDays,
		CreatedAt:     moment(row.CreatedAt),
	}, auth.Role(row.MembershipRole), nil
}

func createWorkspaceTx(ctx context.Context, q *sqlcgen.Queries, name, slug string, ownerUserID uuid.UUID) (Workspace, error) {
	chosen, err := resolveSlug(ctx, q, name, slug)
	if err != nil {
		return Workspace{}, err
	}

	row, err := q.CreateWorkspace(ctx, sqlcgen.CreateWorkspaceParams{
		Name:          name,
		Slug:          chosen,
		BotName:       defaultBotName,
		RetentionDays: defaultRetentionDays,
	})
	if err != nil {
		return Workspace{}, fmt.Errorf("create workspace: %w", err)
	}

	if _, err := q.CreateMembership(ctx, sqlcgen.CreateMembershipParams{
		WorkspaceID: row.ID,
		UserID:      ownerUserID,
		Role:        string(auth.RoleOwner),
	}); err != nil {
		return Workspace{}, fmt.Errorf("create owner membership: %w", err)
	}

	return workspaceFromRow(row), nil
}

func resolveSlug(ctx context.Context, q *sqlcgen.Queries, name, requested string) (string, error) {
	if requested != "" {
		taken, err := q.WorkspaceSlugExists(ctx, requested)
		if err != nil {
			return "", fmt.Errorf("check workspace slug: %w", err)
		}
		if taken {
			return "", ErrSlugTaken
		}
		return requested, nil
	}

	base := Slugify(name)
	if base == "" {
		base = fallbackSlug
	}
	candidate := base
	for attempt := 0; attempt < slugAttempts; attempt++ {
		taken, err := q.WorkspaceSlugExists(ctx, candidate)
		if err != nil {
			return "", fmt.Errorf("check workspace slug: %w", err)
		}
		if !taken {
			return candidate, nil
		}
		candidate, err = slugWithSuffix(base)
		if err != nil {
			return "", err
		}
	}
	return "", ErrSlugTaken
}

func personalWorkspaceName(user User) string {
	if name := strings.TrimSpace(user.Name); name != "" {
		return name + "'s workspace"
	}
	if user.Email != "" {
		local, _, found := strings.Cut(user.Email, "@")
		if found && local != "" {
			return local + "'s workspace"
		}
	}
	return fallbackWorkspaceName
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}
