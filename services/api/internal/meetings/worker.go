package meetings

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
	"github.com/judeotine/afterword/services/api/internal/storage"
)

type WorkerCreateParams struct {
	WorkspaceID    uuid.UUID
	Title          string
	Platform       string
	StartedAt      *time.Time
	DurationS      int32
	AudioExtension string
	AudioSizeBytes int64
}

func (s *Service) CreateForWorker(ctx context.Context, params WorkerCreateParams) (Created, error) {
	if params.WorkspaceID == uuid.Nil {
		return Created{}, fmt.Errorf("worker create: a workspace is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Created{}, fmt.Errorf("begin worker create transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx))
	}()

	queries := s.queries.WithTx(tx)
	row, err := queries.CreateMeeting(ctx, sqlcgen.CreateMeetingParams{
		WorkspaceID:  params.WorkspaceID,
		OwnerUserID:  nil,
		Title:        params.Title,
		Source:       SourceBot,
		Platform:     optionalText(params.Platform),
		StartedAt:    optionalTimestamp(params.StartedAt),
		DurationS:    params.DurationS,
		ConsentState: "unknown",
		Visibility:   VisibilityWorkspace,
		FolderID:     nil,
		Status:       StatusPending,
	})
	if err != nil {
		return Created{}, fmt.Errorf("create worker meeting: %w", err)
	}

	audioKey := storage.AudioKey(params.WorkspaceID, row.ID, params.AudioExtension)
	transcriptKey := storage.TranscriptKey(params.WorkspaceID, row.ID)

	updated, err := queries.UpdateMeeting(ctx, sqlcgen.UpdateMeetingParams{
		ID:               row.ID,
		WorkspaceID:      params.WorkspaceID,
		AudioObject:      &audioKey,
		TranscriptObject: &transcriptKey,
	})
	if err != nil {
		return Created{}, fmt.Errorf("record worker meeting objects: %w", err)
	}

	meeting := meetingFromRow(updated)
	upload, err := s.uploadTargets(ctx, meeting, params.AudioSizeBytes)
	if err != nil {
		return Created{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Created{}, fmt.Errorf("commit worker create transaction: %w", err)
	}
	return Created{Meeting: meeting, Upload: upload}, nil
}
