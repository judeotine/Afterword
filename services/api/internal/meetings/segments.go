package meetings

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/judeotine/afterword/services/api/internal/auth"
	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

var segmentColumns = []string{"meeting_id", "seq", "speaker", "start_s", "end_s", "text"}

func (s *Service) ReplaceSegments(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, segments []Segment) (int, error) {
	if len(segments) > MaxSegments {
		return 0, ErrTooManySegments
	}
	if _, err := s.manageable(ctx, actor, meetingID); err != nil {
		return 0, err
	}
	seen := make(map[int32]struct{}, len(segments))
	for _, segment := range segments {
		if _, exists := seen[segment.Seq]; exists {
			return 0, ErrDuplicateSequence
		}
		seen[segment.Seq] = struct{}{}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin segment replace: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	if _, err := queries.DeleteSegmentsForMeeting(ctx, sqlcgen.DeleteSegmentsForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: actor.WorkspaceID,
	}); err != nil {
		return 0, fmt.Errorf("clear transcript segments: %w", err)
	}

	if len(segments) > 0 {
		source := pgx.CopyFromSlice(len(segments), func(index int) ([]any, error) {
			segment := segments[index]
			return []any{meetingID, segment.Seq, optionalText(segment.Speaker), segment.StartS, segment.EndS, segment.Text}, nil
		})
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"transcript_segments"}, segmentColumns, source); err != nil {
			return 0, fmt.Errorf("insert transcript segments: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit segment replace: %w", err)
	}
	return len(segments), nil
}

func (s *Service) ListSegments(ctx context.Context, actor auth.Membership, meetingID uuid.UUID, afterSeq *int32, pageSize int32) (SegmentPage, error) {
	if _, err := s.viewable(ctx, actor, meetingID); err != nil {
		return SegmentPage{}, err
	}
	return s.segments(ctx, actor.WorkspaceID, meetingID, afterSeq, pageSize)
}

func (s *Service) segments(ctx context.Context, workspaceID, meetingID uuid.UUID, afterSeq *int32, pageSize int32) (SegmentPage, error) {
	size := pageSize
	if size <= 0 {
		size = DefaultPageSize
	}
	if size > MaxPageSize {
		size = MaxPageSize
	}

	total, err := s.queries.CountSegmentsForMeeting(ctx, sqlcgen.CountSegmentsForMeetingParams{
		MeetingID:   meetingID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return SegmentPage{}, fmt.Errorf("count transcript segments: %w", err)
	}

	rows, err := s.queries.ListSegmentsBySeq(ctx, sqlcgen.ListSegmentsBySeqParams{
		MeetingID:   meetingID,
		WorkspaceID: workspaceID,
		AfterSeq:    afterSeq,
		PageSize:    size + 1,
	})
	if err != nil {
		return SegmentPage{}, fmt.Errorf("list transcript segments: %w", err)
	}

	page := SegmentPage{Segments: make([]StoredSegment, 0, len(rows)), Total: total}
	for index, row := range rows {
		if int32(index) == size {
			last := page.Segments[len(page.Segments)-1].Seq
			page.NextSeq = &last
			break
		}
		page.Segments = append(page.Segments, StoredSegment{
			ID:        row.ID,
			MeetingID: row.MeetingID,
			Segment: Segment{
				Seq:     row.Seq,
				Speaker: text(row.Speaker),
				StartS:  row.StartS,
				EndS:    row.EndS,
				Text:    row.Text,
			},
			CreatedAt: moment(row.CreatedAt),
		})
	}
	return page, nil
}
