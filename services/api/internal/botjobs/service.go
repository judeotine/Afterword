package botjobs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

const (
	StatusScheduled = "scheduled"

	PlatformMeet  = "meet"
	PlatformZoom  = "zoom"
	PlatformTeams = "teams"

	DefaultPageSize  int32 = 25
	MaxPageSize      int32 = 100
	MaxURLLength           = 2048
	MaxBotNameLength       = 80
	MaxLeadTime            = 90 * 24 * time.Hour
	ScheduleGrace          = 5 * time.Minute
)

var (
	ErrJobNotFound      = errors.New("botjobs: bot job not found")
	ErrInvalidURL       = errors.New("botjobs: the meeting url is not usable")
	ErrUnknownPlatform  = errors.New("botjobs: the meeting platform could not be determined")
	ErrPlatformMismatch = errors.New("botjobs: the meeting url does not match the platform")
	ErrScheduleTooFar   = errors.New("botjobs: the meeting is scheduled too far ahead")
	ErrScheduleInPast   = errors.New("botjobs: the meeting is scheduled in the past")
	ErrInvalidCursor    = errors.New("botjobs: cursor is not valid")
)

var Platforms = []string{PlatformMeet, PlatformZoom, PlatformTeams}

type BotJob struct {
	ID               uuid.UUID
	WorkspaceID      uuid.UUID
	MeetingURL       string
	Platform         string
	BotName          string
	ScheduledAt      time.Time
	Status           string
	EstimatedMinutes int32
	MinutesUsed      int32
	Error            string
	CreatedAt        time.Time
}

type CreateParams struct {
	MeetingURL       string
	Platform         string
	BotName          string
	ScheduledAt      *time.Time
	EstimatedMinutes int32
}

type ServiceOptions struct {
	Pool  *pgxpool.Pool
	Clock func() time.Time
}

type Service struct {
	queries *sqlcgen.Queries
	clock   func() time.Time
}

func NewService(options ServiceOptions) (*Service, error) {
	if options.Pool == nil {
		return nil, errors.New("botjobs: a database pool is required")
	}
	clock := options.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	return &Service{queries: sqlcgen.New(options.Pool), clock: clock}, nil
}

func PlatformFor(meetingURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(meetingURL))
	if err != nil || parsed.Host == "" {
		return "", ErrInvalidURL
	}
	if parsed.Scheme != "https" {
		return "", ErrInvalidURL
	}
	host := strings.ToLower(parsed.Hostname())
	switch {
	case host == "meet.google.com":
		return PlatformMeet, nil
	case host == "zoom.us" || strings.HasSuffix(host, ".zoom.us"):
		return PlatformZoom, nil
	case host == "teams.microsoft.com" || host == "teams.live.com" || strings.HasSuffix(host, ".teams.microsoft.com"):
		return PlatformTeams, nil
	default:
		return "", ErrUnknownPlatform
	}
}

func (s *Service) Resolve(params CreateParams) (CreateParams, error) {
	meetingURL := strings.TrimSpace(params.MeetingURL)
	if meetingURL == "" || len(meetingURL) > MaxURLLength {
		return CreateParams{}, ErrInvalidURL
	}

	detected, err := PlatformFor(meetingURL)
	requested := strings.ToLower(strings.TrimSpace(params.Platform))
	switch {
	case errors.Is(err, ErrInvalidURL):
		return CreateParams{}, err
	case err != nil && requested == "":
		return CreateParams{}, err
	case err != nil:
		return CreateParams{}, ErrUnknownPlatform
	case requested == "":
		requested = detected
	case detected != requested:
		return CreateParams{}, ErrPlatformMismatch
	}

	now := s.clock().UTC()
	scheduledAt := now
	if params.ScheduledAt != nil {
		scheduledAt = params.ScheduledAt.UTC()
		switch {
		case scheduledAt.After(now.Add(MaxLeadTime)):
			return CreateParams{}, ErrScheduleTooFar
		case scheduledAt.Before(now.Add(-ScheduleGrace)):
			return CreateParams{}, ErrScheduleInPast
		}
	}

	return CreateParams{
		MeetingURL:       meetingURL,
		Platform:         requested,
		BotName:          strings.TrimSpace(params.BotName),
		ScheduledAt:      &scheduledAt,
		EstimatedMinutes: params.EstimatedMinutes,
	}, nil
}

func (s *Service) Create(ctx context.Context, workspaceID uuid.UUID, params CreateParams) (BotJob, error) {
	var botName *string
	if params.BotName != "" {
		name := params.BotName
		botName = &name
	}
	scheduledAt := s.clock().UTC()
	if params.ScheduledAt != nil {
		scheduledAt = params.ScheduledAt.UTC()
	}

	row, err := s.queries.CreateBotJob(ctx, sqlcgen.CreateBotJobParams{
		WorkspaceID:      workspaceID,
		MeetingUrl:       params.MeetingURL,
		Platform:         params.Platform,
		ScheduledAt:      pgtype.Timestamptz{Time: scheduledAt, Valid: true},
		Status:           StatusScheduled,
		EstimatedMinutes: params.EstimatedMinutes,
		BotName:          botName,
	})
	if err != nil {
		return BotJob{}, fmt.Errorf("create bot job: %w", err)
	}
	return newBotJob(row), nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id uuid.UUID) (BotJob, error) {
	row, err := s.queries.GetBotJob(ctx, sqlcgen.GetBotJobParams{ID: id, WorkspaceID: workspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BotJob{}, ErrJobNotFound
		}
		return BotJob{}, fmt.Errorf("read bot job: %w", err)
	}
	return newBotJob(row), nil
}

func (s *Service) List(ctx context.Context, workspaceID uuid.UUID, cursor string, pageSize int32) ([]BotJob, string, error) {
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	params := sqlcgen.ListBotJobsByWorkspaceParams{WorkspaceID: workspaceID, PageSize: pageSize + 1}
	if strings.TrimSpace(cursor) != "" {
		at, id, err := DecodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		params.CursorCreatedAt = pgtype.Timestamptz{Time: at, Valid: true}
		params.CursorID = &id
	}

	rows, err := s.queries.ListBotJobsByWorkspace(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("list bot jobs: %w", err)
	}

	next := ""
	if int32(len(rows)) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = EncodeCursor(last.CreatedAt.Time, last.ID)
	}

	jobs := make([]BotJob, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, newBotJob(row))
	}
	return jobs, next, nil
}

func newBotJob(row sqlcgen.BotJob) BotJob {
	job := BotJob{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		MeetingURL:       row.MeetingUrl,
		Platform:         row.Platform,
		Status:           row.Status,
		EstimatedMinutes: row.EstimatedMinutes,
		MinutesUsed:      row.MinutesUsed,
	}
	if row.BotName != nil {
		job.BotName = *row.BotName
	}
	if row.Error != nil {
		job.Error = *row.Error
	}
	if row.ScheduledAt.Valid {
		job.ScheduledAt = row.ScheduledAt.Time.UTC()
	}
	if row.CreatedAt.Valid {
		job.CreatedAt = row.CreatedAt.Time.UTC()
	}
	return job
}
