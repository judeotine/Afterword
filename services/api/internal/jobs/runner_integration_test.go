//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/judeotine/afterword/services/api/internal/jobs"
)

func TestRunnerDispatchesByKind(t *testing.T) {
	ctx, _, queue := setup(t)

	var (
		mu   sync.Mutex
		seen = map[string][]string{}
	)
	record := func(kind string) jobs.Handler {
		return func(_ context.Context, job *jobs.Job) error {
			mu.Lock()
			seen[kind] = append(seen[kind], string(job.Payload))
			mu.Unlock()
			return nil
		}
	}

	runner := jobs.NewRunner(queue,
		jobs.WithWorkerID("runner-1"),
		jobs.WithPollInterval(10*time.Millisecond),
		jobs.WithJitter(0),
	)
	if err := runner.Register("transcribe", 1, record("transcribe")); err != nil {
		t.Fatalf("Register transcribe: %v", err)
	}
	if err := runner.Register("summarise", 1, record("summarise")); err != nil {
		t.Fatalf("Register summarise: %v", err)
	}
	if err := runner.Register("summarise", 1, record("summarise")); !errors.Is(err, jobs.ErrKindRegistered) {
		t.Fatalf("duplicate Register = %v, want ErrKindRegistered", err)
	}

	for _, kind := range []string{"transcribe", "summarise", "unhandled"} {
		if _, err := queue.Enqueue(ctx, kind, kind, time.Time{}, kind); err != nil {
			t.Fatalf("Enqueue %s: %v", kind, err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- runner.Run(runCtx) }()

	waitFor(t, 20*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(seen["transcribe"]) == 1 && len(seen["summarise"]) == 1
	})
	cancel()
	if err := <-stopped; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}

	if status := jobStatus(ctx, t, queue, "unhandled", "unhandled"); status != jobs.StatusPending {
		t.Errorf("unhandled job status = %q, want %q", status, jobs.StatusPending)
	}
	if status := jobStatus(ctx, t, queue, "transcribe", "transcribe"); status != jobs.StatusSucceeded {
		t.Errorf("transcribe job status = %q, want %q", status, jobs.StatusSucceeded)
	}
}

func TestRunnerHonoursPerKindConcurrency(t *testing.T) {
	ctx, _, queue := setup(t)

	var (
		inFlight int64
		peak     int64
		done     int64
	)
	handler := func(_ context.Context, _ *jobs.Job) error {
		current := atomic.AddInt64(&inFlight, 1)
		for {
			observed := atomic.LoadInt64(&peak)
			if current <= observed || atomic.CompareAndSwapInt64(&peak, observed, current) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		atomic.AddInt64(&done, 1)
		return nil
	}

	runner := jobs.NewRunner(queue,
		jobs.WithWorkerID("runner-2"),
		jobs.WithPollInterval(5*time.Millisecond),
		jobs.WithJitter(0),
	)
	if err := runner.Register("transcribe", 2, handler); err != nil {
		t.Fatalf("Register: %v", err)
	}

	const total = 12
	for i := 0; i < total; i++ {
		if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, fmt.Sprintf("job-%d", i)); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- runner.Run(runCtx) }()

	waitFor(t, 30*time.Second, func() bool { return atomic.LoadInt64(&done) == total })
	cancel()
	if err := <-stopped; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}

	if got := atomic.LoadInt64(&peak); got > 2 {
		t.Errorf("peak concurrency = %d, want at most 2", got)
	}
}

func TestRunnerRetriesFailingHandlerUntilDeadLetter(t *testing.T) {
	ctx, _, queue := setup(t)
	queue.SetBackoff(jobs.Backoff{Base: time.Millisecond, Factor: 1, Max: time.Millisecond})

	var attempts int64
	runner := jobs.NewRunner(queue,
		jobs.WithWorkerID("runner-3"),
		jobs.WithPollInterval(5*time.Millisecond),
		jobs.WithJitter(0),
	)
	err := runner.Register("transcribe", 1, func(_ context.Context, _ *jobs.Job) error {
		atomic.AddInt64(&attempts, 1)
		return errors.New("handler failed")
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := queue.Enqueue(ctx, "transcribe", nil, time.Time{}, "doomed"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- runner.Run(runCtx) }()

	waitFor(t, 30*time.Second, func() bool {
		return jobStatus(ctx, t, queue, "transcribe", "doomed") == jobs.StatusDead
	})
	cancel()
	if err := <-stopped; err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("Run: %v", err)
	}

	if got := atomic.LoadInt64(&attempts); got != jobs.DefaultMaxAttempts {
		t.Errorf("handler ran %d times, want %d", got, jobs.DefaultMaxAttempts)
	}
}

func TestRunnerRequiresAtLeastOneHandler(t *testing.T) {
	_, _, queue := setup(t)
	runner := jobs.NewRunner(queue)
	if err := runner.Run(context.Background()); !errors.Is(err, jobs.ErrNoHandlers) {
		t.Fatalf("Run = %v, want ErrNoHandlers", err)
	}
}

func jobStatus(ctx context.Context, t *testing.T, queue *jobs.Queue, kind string, idempotencyKey string) jobs.Status {
	t.Helper()
	job, err := queue.GetByIdempotencyKey(ctx, kind, idempotencyKey)
	if err != nil {
		t.Fatalf("GetByIdempotencyKey %q %q: %v", kind, idempotencyKey, err)
	}
	return job.Status
}

func waitFor(t *testing.T, limit time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition was not met before the deadline")
}
