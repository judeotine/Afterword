package sqlcgen

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

const attachBotJobMeeting = `-- name: AttachBotJobMeeting :one
UPDATE bot_jobs SET
    meeting_id = $1
WHERE id = $2
RETURNING id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id
`

type AttachBotJobMeetingParams struct {
	MeetingID *uuid.UUID `json:"meeting_id"`
	ID        uuid.UUID  `json:"id"`
}

func (q *Queries) AttachBotJobMeeting(ctx context.Context, arg AttachBotJobMeetingParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, attachBotJobMeeting, arg.MeetingID, arg.ID)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const claimNextBotJob = `-- name: ClaimNextBotJob :one
UPDATE bot_jobs SET
    status = 'claimed',
    worker_id = $1
WHERE id = (
    SELECT candidate.id FROM bot_jobs candidate
    WHERE candidate.status = 'scheduled' AND candidate.scheduled_at <= $2
    ORDER BY candidate.scheduled_at, candidate.id
    FOR UPDATE SKIP LOCKED
    LIMIT 1
)
RETURNING id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id
`

type ClaimNextBotJobParams struct {
	WorkerID *string            `json:"worker_id"`
	Now      pgtype.Timestamptz `json:"now"`
}

func (q *Queries) ClaimNextBotJob(ctx context.Context, arg ClaimNextBotJobParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, claimNextBotJob, arg.WorkerID, arg.Now)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const createBotJob = `-- name: CreateBotJob :one
INSERT INTO bot_jobs (workspace_id, meeting_url, platform, scheduled_at, status, worker_id,
                      estimated_minutes, bot_name)
VALUES ($1, $2, $3, $4,
        $5, $6, $7, $8)
RETURNING id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id
`

type CreateBotJobParams struct {
	WorkspaceID      uuid.UUID          `json:"workspace_id"`
	MeetingUrl       string             `json:"meeting_url"`
	Platform         string             `json:"platform"`
	ScheduledAt      pgtype.Timestamptz `json:"scheduled_at"`
	Status           string             `json:"status"`
	WorkerID         *string            `json:"worker_id"`
	EstimatedMinutes int32              `json:"estimated_minutes"`
	BotName          *string            `json:"bot_name"`
}

func (q *Queries) CreateBotJob(ctx context.Context, arg CreateBotJobParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, createBotJob,
		arg.WorkspaceID,
		arg.MeetingUrl,
		arg.Platform,
		arg.ScheduledAt,
		arg.Status,
		arg.WorkerID,
		arg.EstimatedMinutes,
		arg.BotName,
	)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const deleteBotJob = `-- name: DeleteBotJob :execrows
DELETE FROM bot_jobs
WHERE id = $1 AND workspace_id = $2
`

type DeleteBotJobParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) DeleteBotJob(ctx context.Context, arg DeleteBotJobParams) (int64, error) {
	result, err := q.db.Exec(ctx, deleteBotJob, arg.ID, arg.WorkspaceID)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

const getBotJob = `-- name: GetBotJob :one
SELECT id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id FROM bot_jobs
WHERE id = $1 AND workspace_id = $2
`

type GetBotJobParams struct {
	ID          uuid.UUID `json:"id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
}

func (q *Queries) GetBotJob(ctx context.Context, arg GetBotJobParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, getBotJob, arg.ID, arg.WorkspaceID)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const getBotJobByID = `-- name: GetBotJobByID :one
SELECT id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id FROM bot_jobs WHERE id = $1
`

func (q *Queries) GetBotJobByID(ctx context.Context, id uuid.UUID) (BotJob, error) {
	row := q.db.QueryRow(ctx, getBotJobByID, id)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const listBotJobsByWorkspace = `-- name: ListBotJobsByWorkspace :many
SELECT id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id FROM bot_jobs
WHERE workspace_id = $1
  AND (
      $2::timestamptz IS NULL
      OR (created_at, id) < ($2::timestamptz, $3::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT $4
`

type ListBotJobsByWorkspaceParams struct {
	WorkspaceID     uuid.UUID          `json:"workspace_id"`
	CursorCreatedAt pgtype.Timestamptz `json:"cursor_created_at"`
	CursorID        *uuid.UUID         `json:"cursor_id"`
	PageSize        int32              `json:"page_size"`
}

func (q *Queries) ListBotJobsByWorkspace(ctx context.Context, arg ListBotJobsByWorkspaceParams) ([]BotJob, error) {
	rows, err := q.db.Query(ctx, listBotJobsByWorkspace,
		arg.WorkspaceID,
		arg.CursorCreatedAt,
		arg.CursorID,
		arg.PageSize,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []BotJob{}
	for rows.Next() {
		var i BotJob
		if err := rows.Scan(
			&i.ID,
			&i.WorkspaceID,
			&i.MeetingUrl,
			&i.Platform,
			&i.ScheduledAt,
			&i.Status,
			&i.WorkerID,
			&i.MinutesUsed,
			&i.ConsentAnnouncedAt,
			&i.Error,
			&i.CreatedAt,
			&i.EstimatedMinutes,
			&i.BotName,
			&i.MeetingID,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const updateBotJob = `-- name: UpdateBotJob :one
UPDATE bot_jobs SET
    status = COALESCE($1, status),
    worker_id = COALESCE($2, worker_id),
    minutes_used = COALESCE($3, minutes_used),
    consent_announced_at = COALESCE($4, consent_announced_at),
    error = COALESCE($5, error)
WHERE id = $6 AND workspace_id = $7
RETURNING id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id
`

type UpdateBotJobParams struct {
	Status             *string            `json:"status"`
	WorkerID           *string            `json:"worker_id"`
	MinutesUsed        *int32             `json:"minutes_used"`
	ConsentAnnouncedAt pgtype.Timestamptz `json:"consent_announced_at"`
	Error              *string            `json:"error"`
	ID                 uuid.UUID          `json:"id"`
	WorkspaceID        uuid.UUID          `json:"workspace_id"`
}

func (q *Queries) UpdateBotJob(ctx context.Context, arg UpdateBotJobParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, updateBotJob,
		arg.Status,
		arg.WorkerID,
		arg.MinutesUsed,
		arg.ConsentAnnouncedAt,
		arg.Error,
		arg.ID,
		arg.WorkspaceID,
	)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}

const updateBotJobByWorker = `-- name: UpdateBotJobByWorker :one
UPDATE bot_jobs SET
    status = COALESCE($1, status),
    minutes_used = COALESCE($2, minutes_used),
    consent_announced_at = COALESCE($3, consent_announced_at),
    error = COALESCE($4, error)
WHERE id = $5 AND worker_id = $6
RETURNING id, workspace_id, meeting_url, platform, scheduled_at, status, worker_id, minutes_used, consent_announced_at, error, created_at, estimated_minutes, bot_name, meeting_id
`

type UpdateBotJobByWorkerParams struct {
	Status             *string            `json:"status"`
	MinutesUsed        *int32             `json:"minutes_used"`
	ConsentAnnouncedAt pgtype.Timestamptz `json:"consent_announced_at"`
	Error              *string            `json:"error"`
	ID                 uuid.UUID          `json:"id"`
	WorkerID           *string            `json:"worker_id"`
}

func (q *Queries) UpdateBotJobByWorker(ctx context.Context, arg UpdateBotJobByWorkerParams) (BotJob, error) {
	row := q.db.QueryRow(ctx, updateBotJobByWorker,
		arg.Status,
		arg.MinutesUsed,
		arg.ConsentAnnouncedAt,
		arg.Error,
		arg.ID,
		arg.WorkerID,
	)
	var i BotJob
	err := row.Scan(
		&i.ID,
		&i.WorkspaceID,
		&i.MeetingUrl,
		&i.Platform,
		&i.ScheduledAt,
		&i.Status,
		&i.WorkerID,
		&i.MinutesUsed,
		&i.ConsentAnnouncedAt,
		&i.Error,
		&i.CreatedAt,
		&i.EstimatedMinutes,
		&i.BotName,
		&i.MeetingID,
	)
	return i, err
}
