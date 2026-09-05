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

const (
	DefaultShareRateWindow = time.Minute
	DefaultShareRateLimit  = 120
)

type ShareParams struct {
	Permission string
	ExpiresAt  *time.Time
}

func (s *Service) CreateShareLink(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, params ShareParams) (ShareLink, Meeting, error) {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return ShareLink{}, Meeting{}, err
	}
	permission := params.Permission
	if permission == "" {
		permission = PermissionView
	}
	token, err := auth.NewOpaqueToken(ShareTokenBytes)
	if err != nil {
		return ShareLink{}, Meeting{}, fmt.Errorf("generate share token: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return ShareLink{}, Meeting{}, fmt.Errorf("begin share transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	row, err := queries.CreateShareLink(ctx, sqlcgen.CreateShareLinkParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
		TokenHash:   HashShareToken(token),
		Permission:  permission,
		ExpiresAt:   optionalTimestamp(params.ExpiresAt),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ShareLink{}, Meeting{}, ErrMeetingNotFound
		}
		return ShareLink{}, Meeting{}, fmt.Errorf("create share link: %w", err)
	}

	updated, err := queries.SetMeetingLinkSharing(ctx, sqlcgen.SetMeetingLinkSharingParams{
		ID:                 meetingID,
		WorkspaceID:        actor.WorkspaceID,
		LinkSharingEnabled: true,
	})
	if err != nil {
		return ShareLink{}, Meeting{}, fmt.Errorf("enable link sharing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return ShareLink{}, Meeting{}, fmt.Errorf("commit share transaction: %w", err)
	}

	link := shareLinkFromRow(row)
	link.Token = token
	return link, meetingFromRow(updated), nil
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

func (s *Service) RevokeShareLinks(ctx context.Context, actor auth.Membership, meetingID uuid.UUID) (Meeting, error) {
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return Meeting{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Meeting{}, fmt.Errorf("begin revoke transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	if _, err := queries.DeleteShareLinksForMeeting(ctx, sqlcgen.DeleteShareLinksForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
	}); err != nil {
		return Meeting{}, fmt.Errorf("revoke share links: %w", err)
	}

	updated, err := queries.SetMeetingLinkSharing(ctx, sqlcgen.SetMeetingLinkSharingParams{
		ID:                 meetingID,
		WorkspaceID:        actor.WorkspaceID,
		LinkSharingEnabled: false,
	})
	if err != nil {
		return Meeting{}, fmt.Errorf("disable link sharing: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Meeting{}, fmt.Errorf("commit revoke transaction: %w", err)
	}
	return meetingFromRow(updated), nil
}

func (s *Service) Shared(ctx context.Context, token string) (Shared, error) {
	row, err := s.queries.GetMeetingByShareTokenHash(ctx, HashShareToken(token))
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
	if !meeting.LinkSharing {
		return Shared{}, ErrShareLinkClosed
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

func (s *Service) AllowSharedRequest(ctx context.Context, requestIP string) error {
	if requestIP == "" {
		return nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin share rate limit transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	if err := queries.LockShareLinkIP(ctx, requestIP); err != nil {
		return fmt.Errorf("lock share link address: %w", err)
	}

	now := s.clock()
	_, err = queries.TryRecordShareLinkRequest(ctx, sqlcgen.TryRecordShareLinkRequestParams{
		RequestIp:    requestIP,
		RecordedAt:   optionalTimestamp(&now),
		Since:        optionalTimestamp(timePointer(now.Add(-s.shareWindow))),
		RequestLimit: s.shareLimit,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRateLimited
		}
		return fmt.Errorf("record share link request: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit share rate limit transaction: %w", err)
	}
	return nil
}

func (s *Service) SweepShareLinkRequests(ctx context.Context) (int64, error) {
	before := s.clock().Add(-DefaultShareLinkRequestRetention)
	removed, err := s.queries.DeleteExpiredShareLinkRequests(ctx, optionalTimestamp(&before))
	if err != nil {
		return 0, fmt.Errorf("sweep share link requests: %w", err)
	}
	return removed, nil
}

func timePointer(at time.Time) *time.Time {
	return &at
}
