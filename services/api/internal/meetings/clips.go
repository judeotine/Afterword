package meetings

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

const (
	MaxClipTitleLength = 300
	ClipTokenBytes     = 24
)

var (
	ErrClipNotFound    = errors.New("meetings: clip not found")
	ErrClipBounds      = errors.New("meetings: clip end must be after its start")
	ErrClipNoAudio     = errors.New("meetings: the meeting has no audio to clip")
	ErrClipOutOfBounds = errors.New("meetings: the clip range is outside the meeting")
)

type Clip struct {
	ID         uuid.UUID
	MeetingID  uuid.UUID
	StartS     float64
	EndS       float64
	Title      string
	Object     string
	ShareToken string
	CreatedAt  time.Time
}

type ClipPage struct {
	Clips      []Clip
	NextCursor string
}

func clipFromRow(row sqlcgen.Clip) Clip {
	return Clip{
		ID:         row.ID,
		MeetingID:  row.MeetingID,
		StartS:     row.StartS,
		EndS:       row.EndS,
		Title:      row.Title,
		Object:     text(row.Object),
		ShareToken: text(row.ShareToken),
		CreatedAt:  moment(row.CreatedAt),
	}
}

func (s *Service) CreateClip(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, startS, endS float64, title string) (Clip, error) {
	meeting, err := s.viewable(ctx, actor, meetingID)
	if err != nil {
		return Clip{}, err
	}
	if endS <= startS {
		return Clip{}, ErrClipBounds
	}
	if startS < 0 {
		startS = 0
	}
	if meeting.AudioObject == "" {
		return Clip{}, ErrClipNoAudio
	}
	if meeting.DurationS > 0 && startS >= float64(meeting.DurationS) {
		return Clip{}, ErrClipOutOfBounds
	}

	token, err := auth.NewOpaqueToken(ClipTokenBytes)
	if err != nil {
		return Clip{}, fmt.Errorf("generate clip token: %w", err)
	}

	row, err := s.queries.CreateClip(ctx, sqlcgen.CreateClipParams{
		MeetingID:   meetingID,
		StartS:      startS,
		EndS:        endS,
		Title:       strings.TrimSpace(title),
		Object:      nil,
		ShareToken:  &token,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Clip{}, ErrMeetingNotFound
		}
		return Clip{}, fmt.Errorf("create clip: %w", err)
	}

	clip := clipFromRow(row)
	targetKey := storage.ClipKey(actor.WorkspaceID, meetingID, clip.ID)
	payload := ClipPayload{
		WorkspaceID: actor.WorkspaceID,
		MeetingID:   meetingID,
		ClipID:      clip.ID,
		SourceKey:   meeting.AudioObject,
		TargetKey:   targetKey,
		StartS:      startS,
		EndS:        endS,
	}
	if _, err := s.jobs.Enqueue(ctx, KindClip, payload, time.Time{}, clipJobKey(clip.ID)); err != nil {
		s.logger.Error().Err(err).Str("clip_id", clip.ID.String()).Msg("clip cut job could not be queued")
	}
	return clip, nil
}

func (s *Service) ListClips(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, cursor string, pageSize int32) (ClipPage, error) {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return ClipPage{}, err
	}
	size := pageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}
	params := sqlcgen.ListClipsByWorkspaceParams{
		WorkspaceID: actor.WorkspaceID,
		MeetingID:   &meetingID,
		PageSize:    size + 1,
	}
	if cursor != "" {
		at, id, err := DecodeCursor(cursor)
		if err != nil {
			return ClipPage{}, err
		}
		params.CursorCreatedAt = optionalTimestamp(&at)
		params.CursorID = &id
	}
	rows, err := s.queries.ListClipsByWorkspace(ctx, params)
	if err != nil {
		return ClipPage{}, fmt.Errorf("list clips: %w", err)
	}
	page := ClipPage{Clips: make([]Clip, 0, len(rows))}
	for index, row := range rows {
		if int32(index) == size {
			last := page.Clips[len(page.Clips)-1]
			page.NextCursor = EncodeCursor(last.CreatedAt, last.ID)
			break
		}
		page.Clips = append(page.Clips, clipFromRow(row))
	}
	return page, nil
}

func (s *Service) DeleteClip(ctx context.Context, actor auth.Membership, meetingID, clipID uuid.UUID) error {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return err
	}
	existing, err := s.queries.GetClip(ctx, sqlcgen.GetClipParams{ID: clipID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClipNotFound
		}
		return fmt.Errorf("load clip: %w", err)
	}
	if existing.MeetingID != meetingID {
		return ErrClipNotFound
	}
	affected, err := s.queries.DeleteClip(ctx, sqlcgen.DeleteClipParams{ID: clipID, WorkspaceID: actor.WorkspaceID})
	if err != nil {
		return fmt.Errorf("delete clip: %w", err)
	}
	if affected == 0 {
		return ErrClipNotFound
	}
	if existing.Object != nil && *existing.Object != "" {
		s.schedulePurge(ctx, PurgePayload{
			WorkspaceID: actor.WorkspaceID,
			MeetingID:   meetingID,
			Objects:     []ObjectRef{{Bucket: s.buckets.Clips, Key: *existing.Object}},
		})
	}
	return nil
}

func clipJobKey(clipID uuid.UUID) string {
	return "clip:" + clipID.String()
}
