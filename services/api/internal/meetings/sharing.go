package meetings

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

type ShareParams struct {
	Permission string
	ExpiresAt  *time.Time
}

func (s *Service) CreateShareLink(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, params ShareParams) (ShareLink, error) {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return ShareLink{}, err
	}
	permission := params.Permission
	if permission == "" {
		permission = PermissionView
	}
	token, err := auth.NewOpaqueToken(ShareTokenBytes)
	if err != nil {
		return ShareLink{}, fmt.Errorf("generate share token: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ShareLink{}, fmt.Errorf("begin share transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	row, err := queries.CreateShareLink(ctx, sqlcgen.CreateShareLinkParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
		Token:       token,
		Permission:  permission,
		ExpiresAt:   optionalTimestamp(params.ExpiresAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ShareLink{}, ErrMeetingNotFound
		}
		return ShareLink{}, fmt.Errorf("create share link: %w", err)
	}

	visibility := VisibilityLink
	if _, err := queries.UpdateMeetingDetails(ctx, sqlcgen.UpdateMeetingDetailsParams{
		ID:          meetingID,
		WorkspaceID: actor.WorkspaceID,
		Visibility:  &visibility,
	}); err != nil {
		return ShareLink{}, fmt.Errorf("open meeting for link sharing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ShareLink{}, fmt.Errorf("commit share transaction: %w", err)
	}
	return shareLinkFromRow(row), nil
}

func (s *Service) ListShareLinks(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) ([]ShareLink, error) {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return nil, err
	}
	rows, err := s.queries.ListShareLinksForMeeting(ctx, sqlcgen.ListShareLinksForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
	})
	if err != nil {
		return nil, fmt.Errorf("list share links: %w", err)
	}
	links := make([]ShareLink, 0, len(rows))
	for _, row := range rows {
		links = append(links, shareLinkFromRow(row))
	}
	return links, nil
}

func (s *Service) RevokeShareLinks(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) error {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return err
	}
	if _, err := s.queries.DeleteShareLinksForMeeting(ctx, sqlcgen.DeleteShareLinksForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
	}); err != nil {
		return fmt.Errorf("revoke share links: %w", err)
	}

	visibility := VisibilityPrivate
	if _, err := s.queries.UpdateMeetingDetails(ctx, sqlcgen.UpdateMeetingDetailsParams{
		ID:          meetingID,
		WorkspaceID: actor.WorkspaceID,
		Visibility:  &visibility,
	}); err != nil {
		return fmt.Errorf("close meeting to link sharing: %w", err)
	}
	return nil
}

func (s *Service) Shared(ctx context.Context, token string) (Shared, error) {
	row, err := s.queries.GetMeetingByShareToken(ctx, token)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Shared{}, ErrShareLinkNotFound
		}
		return Shared{}, fmt.Errorf("read share link: %w", err)
	}

	link := shareLinkFromRow(row.ShareLink)
	if link.ExpiresAt != nil && !link.ExpiresAt.After(s.clock()) {
		return Shared{}, ErrShareLinkExpired
	}

	meeting := meetingFromRow(row.Meeting)
	if meeting.Visibility != VisibilityLink {
		return Shared{}, ErrShareLinkNotFound
	}

	downloads, err := s.downloads(ctx, meeting)
	if err != nil {
		return Shared{}, err
	}
	return Shared{Meeting: meeting, Link: link, Downloads: downloads, Permission: link.Permission}, nil
}

func (s *Service) SharedSegments(ctx context.Context, token string, afterSeq *int32, pageSize int32) (SegmentPage, error) {
	shared, err := s.Shared(ctx, token)
	if err != nil {
		return SegmentPage{}, err
	}
	return s.segments(ctx, shared.Meeting.WorkspaceID, shared.Meeting.ID, afterSeq, pageSize)
}
