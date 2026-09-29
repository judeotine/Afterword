package botjobs

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/judeotine/afterword/services/api/internal/db/sqlcgen"
)

var (
	StatusClaimed   = "claimed"
	StatusJoining   = "joining"
	StatusRecording = "recording"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

var workerStatuses = map[string]bool{
	StatusJoining:   true,
	StatusRecording: true,
	StatusCompleted: true,
	StatusFailed:    true,
}

var ErrNoJobAvailable = errors.New("botjobs: no job available to claim")

func (s *Service) ClaimNext(ctx context.Context, workerID string) (BotJob, error) {
	now := pgtype.Timestamptz{Time: s.clock(), Valid: true}
	row, err := s.queries.ClaimNextBotJob(ctx, sqlcgen.ClaimNextBotJobParams{
		WorkerID: &workerID,
		Now:      now,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BotJob{}, ErrNoJobAvailable
		}
		return BotJob{}, fmt.Errorf("claim bot job: %w", err)
	}
	return newBotJob(row), nil
}

func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (BotJob, error) {
	row, err := s.queries.GetBotJobByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BotJob{}, ErrJobNotFound
		}
		return BotJob{}, fmt.Errorf("get bot job: %w", err)
	}
	return newBotJob(row), nil
}

type WorkerUpdate struct {
	Status           string
	MinutesUsed      *int32
	ConsentAnnounced bool
	Error            string
}

func (s *Service) ReportStatus(ctx context.Context, id uuid.UUID, workerID string, update WorkerUpdate) (BotJob, error) {
	params := sqlcgen.UpdateBotJobByWorkerParams{
		ID:       id,
		WorkerID: &workerID,
	}
	if update.Status != "" {
		if !workerStatuses[update.Status] {
			return BotJob{}, fmt.Errorf("botjobs: worker cannot set status %q", update.Status)
		}
		status := update.Status
		params.Status = &status
	}
	if update.MinutesUsed != nil {
		params.MinutesUsed = update.MinutesUsed
	}
	if update.ConsentAnnounced {
		params.ConsentAnnouncedAt = pgtype.Timestamptz{Time: s.clock(), Valid: true}
	}
	if update.Error != "" {
		errText := update.Error
		params.Error = &errText
	}

	row, err := s.queries.UpdateBotJobByWorker(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return BotJob{}, ErrJobNotFound
		}
		return BotJob{}, fmt.Errorf("update bot job by worker: %w", err)
	}
	return newBotJob(row), nil
}
