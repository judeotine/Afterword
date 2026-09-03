package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Status string

const (
	StatusPending   Status = "pending"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusDead      Status = "dead"
)

var (
	ErrNoJob        = errors.New("jobs: no job available")
	ErrNotFound     = errors.New("jobs: job not found")
	ErrNotRunning   = errors.New("jobs: job is not running")
	ErrKindRequired = errors.New("jobs: kind is required")
	ErrNoKinds      = errors.New("jobs: at least one kind is required")
)

const jobColumns = `id, kind, payload, idempotency_key, run_at, attempts, max_attempts,
	locked_by, locked_at, status, last_error, created_at, updated_at`

type Job struct {
	ID             uuid.UUID
	Kind           string
	Payload        json.RawMessage
	IdempotencyKey string
	RunAt          time.Time
	Attempts       int32
	MaxAttempts    int32
	LockedBy       string
	LockedAt       time.Time
	Status         Status
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Queue struct {
	pool *pgxpool.Pool

	mu          sync.RWMutex
	backoff     Backoff
	maxAttempts int32
	clock       func() time.Time
}

type QueueOption func(*Queue)

func WithBackoff(backoff Backoff) QueueOption {
	return func(q *Queue) { q.backoff = backoff }
}

func WithMaxAttempts(attempts int32) QueueOption {
	return func(q *Queue) {
		if attempts > 0 {
			q.maxAttempts = attempts
		}
	}
}

func WithClock(clock func() time.Time) QueueOption {
	return func(q *Queue) {
		if clock != nil {
			q.clock = clock
		}
	}
}

func NewQueue(pool *pgxpool.Pool, opts ...QueueOption) *Queue {
	queue := &Queue{
		pool:        pool,
		backoff:     DefaultBackoff(),
		maxAttempts: DefaultMaxAttempts,
		clock:       func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		opt(queue)
	}
	return queue
}

func (q *Queue) SetBackoff(backoff Backoff) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.backoff = backoff
}

func (q *Queue) SetClock(clock func() time.Time) {
	if clock == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.clock = clock
}

func (q *Queue) Backoff() Backoff {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.backoff
}

func (q *Queue) Now() time.Time {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.clock()
}

func (q *Queue) MaxAttempts() int32 {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.maxAttempts
}

func (q *Queue) Enqueue(ctx context.Context, kind string, payload any, runAt time.Time, idempotencyKey string) (*Job, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return nil, ErrKindRequired
	}
	encoded, err := encodePayload(payload)
	if err != nil {
		return nil, err
	}
	if runAt.IsZero() {
		runAt = q.Now()
	}
	var key *string
	if idempotencyKey != "" {
		key = &idempotencyKey
	}

	const insert = `INSERT INTO jobs (kind, payload, idempotency_key, run_at, max_attempts, status)
VALUES ($1, $2, $3, $4, $5, 'pending')
ON CONFLICT (kind, idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
RETURNING ` + jobColumns

	job, err := scanJob(q.pool.QueryRow(ctx, insert, kind, encoded, key, runAt, q.MaxAttempts()))
	switch {
	case err == nil:
		return job, nil
	case errors.Is(err, pgx.ErrNoRows) && key != nil:
		return q.GetByIdempotencyKey(ctx, kind, idempotencyKey)
	case errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("enqueue job: %w", ErrNotFound)
	default:
		return nil, fmt.Errorf("enqueue job: %w", err)
	}
}

func (q *Queue) Claim(ctx context.Context, workerID string, kinds []string) (*Job, error) {
	if len(kinds) == 0 {
		return nil, ErrNoKinds
	}

	const claim = `UPDATE jobs SET
	status = 'running',
	locked_by = $1,
	locked_at = $2,
	attempts = attempts + 1,
	updated_at = $2
WHERE id = (
	SELECT id FROM jobs
	WHERE status = 'pending' AND run_at <= $2 AND kind = ANY ($3::text[])
	ORDER BY run_at, id
	FOR UPDATE SKIP LOCKED
	LIMIT 1
)
RETURNING ` + jobColumns

	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin claim transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	job, err := scanJob(tx.QueryRow(ctx, claim, workerID, q.Now(), kinds))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoJob
		}
		return nil, fmt.Errorf("claim job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit claim transaction: %w", err)
	}
	return job, nil
}

func (q *Queue) Complete(ctx context.Context, id uuid.UUID) (*Job, error) {
	const complete = `UPDATE jobs SET
	status = 'succeeded',
	locked_by = NULL,
	locked_at = NULL,
	last_error = NULL,
	updated_at = $2
WHERE id = $1 AND status = 'running'
RETURNING ` + jobColumns

	job, err := scanJob(q.pool.QueryRow(ctx, complete, id, q.Now()))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, q.explainMissingRunningJob(ctx, id)
		}
		return nil, fmt.Errorf("complete job: %w", err)
	}
	return job, nil
}

func (q *Queue) Fail(ctx context.Context, id uuid.UUID, cause error) (*Job, error) {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin fail transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var (
		attempts    int32
		maxAttempts int32
		status      Status
	)
	err = tx.QueryRow(ctx, `SELECT attempts, max_attempts, status FROM jobs WHERE id = $1 FOR UPDATE`, id).
		Scan(&attempts, &maxAttempts, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("load job for failure: %w", err)
	}
	if status != StatusRunning {
		return nil, ErrNotRunning
	}

	now := q.Now()
	nextStatus := StatusPending
	runAt := now.Add(q.Backoff().Delay(attempts))
	if attempts >= maxAttempts {
		nextStatus = StatusDead
	}

	message := ""
	if cause != nil {
		message = cause.Error()
	}

	const fail = `UPDATE jobs SET
	status = $2,
	run_at = CASE WHEN $2 = 'dead' THEN run_at ELSE $3::timestamptz END,
	locked_by = NULL,
	locked_at = NULL,
	last_error = $4,
	updated_at = $5
WHERE id = $1
RETURNING ` + jobColumns

	job, err := scanJob(tx.QueryRow(ctx, fail, id, string(nextStatus), runAt, message, now))
	if err != nil {
		return nil, fmt.Errorf("fail job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit fail transaction: %w", err)
	}
	return job, nil
}

func (q *Queue) ReleaseStale(ctx context.Context, lockedOlderThan time.Duration) (int64, error) {
	if lockedOlderThan < 0 {
		lockedOlderThan = 0
	}
	now := q.Now()

	const release = `UPDATE jobs SET
	status = 'pending',
	locked_by = NULL,
	locked_at = NULL,
	updated_at = $2
WHERE status = 'running' AND locked_at IS NOT NULL AND locked_at < $1`

	tag, err := q.pool.Exec(ctx, release, now.Add(-lockedOlderThan), now)
	if err != nil {
		return 0, fmt.Errorf("release stale jobs: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (q *Queue) Get(ctx context.Context, id uuid.UUID) (*Job, error) {
	job, err := scanJob(q.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get job: %w", err)
	}
	return job, nil
}

func (q *Queue) GetByIdempotencyKey(ctx context.Context, kind string, idempotencyKey string) (*Job, error) {
	const query = `SELECT ` + jobColumns + ` FROM jobs WHERE kind = $1 AND idempotency_key = $2`
	job, err := scanJob(q.pool.QueryRow(ctx, query, kind, idempotencyKey))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get job by idempotency key: %w", err)
	}
	return job, nil
}

func (q *Queue) explainMissingRunningJob(ctx context.Context, id uuid.UUID) error {
	var exists bool
	if err := q.pool.QueryRow(ctx, `SELECT true FROM jobs WHERE id = $1`, id).Scan(&exists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("inspect job: %w", err)
	}
	return ErrNotRunning
}

func encodePayload(payload any) (json.RawMessage, error) {
	switch typed := payload.(type) {
	case nil:
		return json.RawMessage("{}"), nil
	case json.RawMessage:
		if len(typed) == 0 {
			return json.RawMessage("{}"), nil
		}
		return typed, nil
	case []byte:
		if len(typed) == 0 {
			return json.RawMessage("{}"), nil
		}
		return json.RawMessage(typed), nil
	default:
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode job payload: %w", err)
		}
		return encoded, nil
	}
}

func scanJob(row pgx.Row) (*Job, error) {
	var (
		job            Job
		idempotencyKey *string
		lockedBy       *string
		lockedAt       *time.Time
		lastError      *string
		status         string
	)
	err := row.Scan(
		&job.ID,
		&job.Kind,
		&job.Payload,
		&idempotencyKey,
		&job.RunAt,
		&job.Attempts,
		&job.MaxAttempts,
		&lockedBy,
		&lockedAt,
		&status,
		&lastError,
		&job.CreatedAt,
		&job.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	job.Status = Status(status)
	if idempotencyKey != nil {
		job.IdempotencyKey = *idempotencyKey
	}
	if lockedBy != nil {
		job.LockedBy = *lockedBy
	}
	if lockedAt != nil {
		job.LockedAt = *lockedAt
	}
	if lastError != nil {
		job.LastError = *lastError
	}
	return &job, nil
}
