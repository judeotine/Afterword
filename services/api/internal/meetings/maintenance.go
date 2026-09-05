package meetings

import (
	"context"
	"fmt"
	"time"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/jobs"
)

const (
	TaskShareLinkRequestSweep        = "share-link-request-sweep"
	LockKeyShareLinkRequestSweep     = int64(0x6166777377656570)
	DefaultShareLinkSweepInterval    = time.Hour
	DefaultShareLinkRequestRetention = 24 * time.Hour

	TaskAbandonedUploadSweep            = "abandoned-upload-sweep"
	LockKeyAbandonedUploadSweep         = int64(0x6166777561626e64)
	DefaultAbandonedUploadSweepInterval = time.Hour
	DefaultAbandonedUploadTTL           = 24 * time.Hour
	abandonedUploadSweepBatch           = int32(500)
)

func MaintenanceTasks(service *Service) []jobs.ScheduledTask {
	if service == nil {
		return nil
	}
	return []jobs.ScheduledTask{{
		Name:     TaskShareLinkRequestSweep,
		Interval: DefaultShareLinkSweepInterval,
		LockKey:  LockKeyShareLinkRequestSweep,
		Run: func(ctx context.Context) error {
			_, err := service.SweepShareLinkRequests(ctx)
			return err
		},
	}, {
		Name:     TaskAbandonedUploadSweep,
		Interval: DefaultAbandonedUploadSweepInterval,
		LockKey:  LockKeyAbandonedUploadSweep,
		Run: func(ctx context.Context) error {
			_, err := service.SweepAbandonedUploads(ctx)
			return err
		},
	}}
}

func (s *Service) SweepAbandonedUploads(ctx context.Context) (int, error) {
	before := s.clock().Add(-s.abandonedUploadTTL)
	rows, err := s.queries.DeleteAbandonedPendingMeetings(ctx, sqlcgen.DeleteAbandonedPendingMeetingsParams{
		OlderThan: optionalTimestamp(&before),
		RowLimit:  abandonedUploadSweepBatch,
	})
	if err != nil {
		return 0, fmt.Errorf("sweep abandoned uploads: %w", err)
	}

	for _, row := range rows {
		meeting := meetingFromRow(row)
		payload := PurgePayload{
			WorkspaceID: meeting.WorkspaceID,
			MeetingID:   meeting.ID,
			Objects:     s.objectsFor(meeting, nil),
		}
		if len(payload.Objects) == 0 {
			continue
		}
		s.schedulePurge(ctx, payload)
	}

	if len(rows) > 0 {
		s.logger.Info().Int("meetings", len(rows)).Dur("ttl", s.abandonedUploadTTL).
			Msg("abandoned uploads were deleted and their objects queued for purge")
	}
	return len(rows), nil
}
