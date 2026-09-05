//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/jobs"
)

func TestEnqueueStoresPendingJob(t *testing.T) {
	ctx, pool, queue := setup(t)

	job, err := queue.Enqueue(ctx, "transcribe", map[string]string{"meeting": "m1"}, time.Time{}, "")
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if job.Status != jobs.StatusPending {
		t.Errorf("status = %q, want %q", job.Status, jobs.StatusPending)
	}
	if job.Attempts != 0 {
		t.Errorf("attempts = %d, want 0", job.Attempts)
	}
	if job.MaxAttempts != jobs.DefaultMaxAttempts {
		t.Errorf("max attempts = %d, want %d", job.MaxAttempts, jobs.DefaultMaxAttempts)
	}
	var payload map[string]string
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		t.Fatalf("decode payload %s: %v", job.Payload, err)
	}
	if payload["meeting"] != "m1" {
		t.Errorf("payload = %s, want the meeting key to be m1", job.Payload)
	}
	if got := countJobs(ctx, t, pool); got != 1 {
		t.Errorf("job count = %d, want 1", got)
	}
}

func TestEnqueueIsIdempotentPerKindAndKey(t *testing.T) {
	ctx, pool, queue := setup(t)

	first, err := queue.Enqueue(ctx, "summarise", nil, time.Time{}, "meeting-1")
	if err != nil {
		t.Fatalf("first Enqueue: %v", err)
	}
	second, err := queue.Enqueue(ctx, "summarise", nil, time.Time{}, "meeting-1")
	if err != nil {
		t.Fatalf("second Enqueue: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("ids differ: %s and %s", first.ID, second.ID)
	}
	other, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, "meeting-1")
	if err != nil {
		t.Fatalf("other kind Enqueue: %v", err)
	}
	if other.ID == first.ID {
		t.Error("the idempotency key is not scoped by kind")
	}
	if got := countJobs(ctx, t, pool); got != 2 {
		t.Errorf("job count = %d, want 2", got)
	}
}

func TestClaimSkipsFutureAndForeignKinds(t *testing.T) {
	ctx, _, queue := setup(t)

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Now().Add(time.Hour), ""); err != nil {
		t.Fatalf("Enqueue future: %v", err)
	}
	if _, err := queue.Enqueue(ctx, "email", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue other kind: %v", err)
	}

	if _, err := queue.Claim(ctx, "worker-1", []string{"transcribe"}); !errors.Is(err, jobs.ErrNoJob) {
		t.Fatalf("Claim = %v, want ErrNoJob", err)
	}

	claimed, err := queue.Claim(ctx, "worker-1", []string{"email"})
	if err != nil {
		t.Fatalf("Claim email: %v", err)
	}
	if claimed.Status != jobs.StatusRunning {
		t.Errorf("status = %q, want %q", claimed.Status, jobs.StatusRunning)
	}
	if claimed.Attempts != 1 {
		t.Errorf("attempts = %d, want 1", claimed.Attempts)
	}
	if claimed.LockedBy != "worker-1" {
		t.Errorf("locked by = %q, want worker-1", claimed.LockedBy)
	}
	if claimed.LockedAt.IsZero() {
		t.Error("locked at was not set")
	}
}

func TestClaimGivesEachJobToExactlyOneWorker(t *testing.T) {
	ctx, _, queue := setup(t)

	const total = 200
	for i := 0; i < total; i++ {
		if _, err := queue.Enqueue(ctx, "transcribe", map[string]int{"n": i}, time.Time{}, fmt.Sprintf("job-%d", i)); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	var (
		mu      sync.Mutex
		claimed = make(map[uuid.UUID]string)
		wg      sync.WaitGroup
	)
	for _, worker := range []string{"worker-a", "worker-b"} {
		wg.Add(1)
		go func(workerID string) {
			defer wg.Done()
			for {
				job, err := queue.Claim(ctx, workerID, []string{"transcribe"})
				if errors.Is(err, jobs.ErrNoJob) {
					return
				}
				if err != nil {
					t.Errorf("%s Claim: %v", workerID, err)
					return
				}
				mu.Lock()
				if previous, seen := claimed[job.ID]; seen {
					t.Errorf("job %s claimed by %s and %s", job.ID, previous, workerID)
				}
				claimed[job.ID] = workerID
				mu.Unlock()
			}
		}(worker)
	}
	wg.Wait()

	if len(claimed) != total {
		t.Fatalf("claimed %d jobs, want %d", len(claimed), total)
	}
	byWorker := map[string]int{}
	for _, workerID := range claimed {
		byWorker[workerID]++
	}
	if len(byWorker) != 2 {
		t.Errorf("workers that claimed at least one job = %d, want 2", len(byWorker))
	}
}

func TestCompleteMarksJobSucceededAndReleasesLock(t *testing.T) {
	ctx, _, queue := setup(t)

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := queue.Claim(ctx, "worker-1", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	done, err := queue.Complete(ctx, claimed.ID, "worker-1")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if done.Status != jobs.StatusSucceeded {
		t.Errorf("status = %q, want %q", done.Status, jobs.StatusSucceeded)
	}
	if done.LockedBy != "" || !done.LockedAt.IsZero() {
		t.Errorf("lock not released: %q %s", done.LockedBy, done.LockedAt)
	}
	if _, err := queue.Complete(ctx, claimed.ID, "worker-1"); !errors.Is(err, jobs.ErrNotRunning) {
		t.Fatalf("second Complete = %v, want ErrNotRunning", err)
	}
}

func TestFailReschedulesWithExponentialBackoff(t *testing.T) {
	ctx, _, queue := setup(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	queue.SetClock(func() time.Time { return now })

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	wants := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute}
	for i, want := range wants {
		claimed, err := queue.Claim(ctx, "worker-1", []string{"transcribe"})
		if err != nil {
			t.Fatalf("Claim %d: %v", i, err)
		}
		failed, err := queue.Fail(ctx, claimed.ID, "worker-1", errors.New("boom"))
		if err != nil {
			t.Fatalf("Fail %d: %v", i, err)
		}
		if failed.Status != jobs.StatusPending {
			t.Fatalf("attempt %d status = %q, want %q", i+1, failed.Status, jobs.StatusPending)
		}
		if got := failed.RunAt.Sub(now); got != want {
			t.Errorf("attempt %d backoff = %s, want %s", i+1, got, want)
		}
		if failed.LastError != "boom" {
			t.Errorf("attempt %d last error = %q, want boom", i+1, failed.LastError)
		}
		now = failed.RunAt
		queue.SetClock(func() time.Time { return now })
	}
}

func TestFailDeadLettersAfterMaxAttempts(t *testing.T) {
	ctx, _, queue := setup(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	queue.SetClock(func() time.Time { return now })

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	var last *jobs.Job
	for attempt := 1; attempt <= jobs.DefaultMaxAttempts; attempt++ {
		claimed, err := queue.Claim(ctx, "worker-1", []string{"transcribe"})
		if err != nil {
			t.Fatalf("Claim attempt %d: %v", attempt, err)
		}
		last, err = queue.Fail(ctx, claimed.ID, "worker-1", fmt.Errorf("failure %d", attempt))
		if err != nil {
			t.Fatalf("Fail attempt %d: %v", attempt, err)
		}
		if attempt < jobs.DefaultMaxAttempts {
			if last.Status != jobs.StatusPending {
				t.Fatalf("attempt %d status = %q, want %q", attempt, last.Status, jobs.StatusPending)
			}
			now = last.RunAt
			queue.SetClock(func() time.Time { return now })
		}
	}

	if last.Status != jobs.StatusDead {
		t.Fatalf("final status = %q, want %q", last.Status, jobs.StatusDead)
	}
	if last.Attempts != jobs.DefaultMaxAttempts {
		t.Errorf("attempts = %d, want %d", last.Attempts, jobs.DefaultMaxAttempts)
	}
	if _, err := queue.Claim(ctx, "worker-1", []string{"transcribe"}); !errors.Is(err, jobs.ErrNoJob) {
		t.Fatalf("dead job was claimed again: %v", err)
	}
}

func TestReleaseStaleReturnsAbandonedJobsToPending(t *testing.T) {
	ctx, pool, queue := setup(t)

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := queue.Claim(ctx, "worker-gone", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	released, err := queue.ReleaseStale(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ReleaseStale: %v", err)
	}
	if released != 0 {
		t.Fatalf("released %d fresh jobs, want 0", released)
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_at = now() - interval '2 hours' WHERE id = $1`, claimed.ID); err != nil {
		t.Fatalf("age the lock: %v", err)
	}

	released, err = queue.ReleaseStale(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ReleaseStale: %v", err)
	}
	if released != 1 {
		t.Fatalf("released %d stale jobs, want 1", released)
	}

	reclaimed, err := queue.Claim(ctx, "worker-new", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim after release: %v", err)
	}
	if reclaimed.ID != claimed.ID {
		t.Errorf("reclaimed %s, want %s", reclaimed.ID, claimed.ID)
	}
	if reclaimed.Attempts != 2 {
		t.Errorf("attempts = %d, want 2", reclaimed.Attempts)
	}
}

func setup(t *testing.T) (context.Context, *pgxpool.Pool, *jobs.Queue) {
	t.Helper()
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx, pool, jobs.NewQueue(pool)
}

func countJobs(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM jobs`).Scan(&count); err != nil {
		t.Fatalf("count jobs: %v", err)
	}
	return count
}

func TestCompleteAndFailRejectAForeignWorker(t *testing.T) {
	ctx, _, queue := setup(t)

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := queue.Claim(ctx, "worker-1", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}

	if _, err := queue.Complete(ctx, claimed.ID, "worker-2"); !errors.Is(err, jobs.ErrLockLost) {
		t.Fatalf("foreign Complete = %v, want ErrLockLost", err)
	}
	if _, err := queue.Fail(ctx, claimed.ID, "worker-2", errors.New("boom")); !errors.Is(err, jobs.ErrLockLost) {
		t.Fatalf("foreign Fail = %v, want ErrLockLost", err)
	}

	current, err := queue.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Status != jobs.StatusRunning || current.LockedBy != "worker-1" {
		t.Fatalf("job = %q locked by %q, want running and worker-1", current.Status, current.LockedBy)
	}

	if _, err := queue.Complete(ctx, claimed.ID, "worker-1"); err != nil {
		t.Fatalf("Complete by the lock holder: %v", err)
	}
}

func TestReleasedWorkerCannotCompleteTheReclaimedJob(t *testing.T) {
	ctx, pool, queue := setup(t)

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, ""); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	stalled, err := queue.Claim(ctx, "worker-slow", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_at = now() - interval '2 hours' WHERE id = $1`, stalled.ID); err != nil {
		t.Fatalf("age the lock: %v", err)
	}
	if _, err := queue.ReleaseStale(ctx, time.Hour); err != nil {
		t.Fatalf("ReleaseStale: %v", err)
	}
	if _, err := queue.Claim(ctx, "worker-fresh", []string{"transcribe"}); err != nil {
		t.Fatalf("Claim after release: %v", err)
	}

	if _, err := queue.Complete(ctx, stalled.ID, "worker-slow"); !errors.Is(err, jobs.ErrLockLost) {
		t.Fatalf("late Complete = %v, want ErrLockLost", err)
	}
	if _, err := queue.Complete(ctx, stalled.ID, "worker-fresh"); err != nil {
		t.Fatalf("Complete by the current holder: %v", err)
	}
}

func TestReleaseStaleDeadLettersExhaustedJobs(t *testing.T) {
	ctx, pool, _ := setup(t)
	queue := jobs.NewQueue(pool, jobs.WithMaxAttempts(1))

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, "exhausted"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	claimed, err := queue.Claim(ctx, "worker-gone", []string{"transcribe"})
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if claimed.Attempts != claimed.MaxAttempts {
		t.Fatalf("attempts = %d, max attempts = %d, want them equal", claimed.Attempts, claimed.MaxAttempts)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET locked_at = now() - interval '2 hours' WHERE id = $1`, claimed.ID); err != nil {
		t.Fatalf("age the lock: %v", err)
	}

	released, err := queue.ReleaseStale(ctx, time.Hour)
	if err != nil {
		t.Fatalf("ReleaseStale: %v", err)
	}
	if released != 1 {
		t.Fatalf("released %d jobs, want 1", released)
	}

	current, err := queue.Get(ctx, claimed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if current.Status != jobs.StatusDead {
		t.Errorf("status = %q, want %q", current.Status, jobs.StatusDead)
	}
	if current.LockedBy != "" {
		t.Errorf("locked by = %q, want the lock to be released", current.LockedBy)
	}
	if current.LastError == "" {
		t.Error("a dead-lettered job carries no explanation")
	}
	if _, err := queue.Claim(ctx, "worker-new", []string{"transcribe"}); !errors.Is(err, jobs.ErrNoJob) {
		t.Fatalf("an exhausted job was revived: %v", err)
	}
}
