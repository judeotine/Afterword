//go:build integration

package jobs_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/judeotine/afterword/services/api/internal/dbtest"
	"github.com/judeotine/afterword/services/api/internal/jobs"
)

const testLockKey int64 = 0x7465737431

func TestOnlyOneSchedulerHoldsTheLeaderLock(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var (
		inside  atomic.Int32
		peak    atomic.Int32
		ran     atomic.Int32
		release = make(chan struct{})
		entered = make(chan struct{}, 4)
	)

	task := func(context.Context) error {
		current := inside.Add(1)
		for {
			observed := peak.Load()
			if current <= observed || peak.CompareAndSwap(observed, current) {
				break
			}
		}
		ran.Add(1)
		entered <- struct{}{}
		<-release
		inside.Add(-1)
		return nil
	}

	const runners = 4
	var (
		wait     sync.WaitGroup
		mu       sync.Mutex
		acquired int
		failures []error
	)
	wait.Add(runners)
	for i := 0; i < runners; i++ {
		go func() {
			defer wait.Done()
			scheduler := jobs.NewScheduler(pool)
			if err := scheduler.Register(jobs.ScheduledTask{
				Name:     "monthly-grant",
				Interval: time.Hour,
				LockKey:  testLockKey,
				Run:      task,
			}); err != nil {
				mu.Lock()
				failures = append(failures, err)
				mu.Unlock()
				return
			}
			ok, err := scheduler.RunTask(ctx, "monthly-grant")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures = append(failures, err)
				return
			}
			if ok {
				acquired++
			}
		}()
	}

	<-entered
	time.Sleep(200 * time.Millisecond)
	close(release)
	wait.Wait()

	for _, err := range failures {
		t.Fatalf("RunTask: %v", err)
	}
	if acquired != 1 {
		t.Fatalf("%d schedulers acquired the leader lock, want 1", acquired)
	}
	if got := peak.Load(); got != 1 {
		t.Fatalf("peak concurrent task runs = %d, want 1", got)
	}
	if got := ran.Load(); got != 1 {
		t.Fatalf("the task ran %d times, want 1", got)
	}
}

func TestTheLeaderLockIsReleasedForTheNextRun(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	var runs atomic.Int32
	scheduler := jobs.NewScheduler(pool)
	if err := scheduler.Register(jobs.ScheduledTask{
		Name:     "retention",
		Interval: time.Hour,
		LockKey:  testLockKey + 1,
		Run: func(context.Context) error {
			runs.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	for i := 0; i < 3; i++ {
		ok, err := scheduler.RunTask(ctx, "retention")
		if err != nil {
			t.Fatalf("RunTask %d: %v", i, err)
		}
		if !ok {
			t.Fatalf("RunTask %d did not acquire the lock", i)
		}
	}
	if got := runs.Load(); got != 3 {
		t.Fatalf("task ran %d times, want 3", got)
	}
}

func TestALeaderLockIsReleasedWhenTheTaskFails(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	boom := errors.New("task failed")
	scheduler := jobs.NewScheduler(pool)
	if err := scheduler.Register(jobs.ScheduledTask{
		Name:     "boom",
		Interval: time.Hour,
		LockKey:  testLockKey + 2,
		Run:      func(context.Context) error { return boom },
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	if _, err := scheduler.RunTask(ctx, "boom"); !errors.Is(err, boom) {
		t.Fatalf("RunTask error = %v, want the task error", err)
	}
	ok, err := scheduler.RunTask(ctx, "boom")
	if !errors.Is(err, boom) {
		t.Fatalf("second RunTask error = %v, want the task error", err)
	}
	if !ok {
		t.Fatal("the leader lock was not released after a failing task")
	}
}

func TestSchedulerRejectsBadRegistrationsAndUnknownTasks(t *testing.T) {
	pool := dbtest.New(t)
	scheduler := jobs.NewScheduler(pool)

	valid := jobs.ScheduledTask{Name: "grant", Interval: time.Hour, LockKey: testLockKey + 3, Run: func(context.Context) error { return nil }}
	if err := scheduler.Register(valid); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := scheduler.Register(valid); !errors.Is(err, jobs.ErrTaskRegistered) {
		t.Fatalf("duplicate Register = %v, want ErrTaskRegistered", err)
	}
	if err := scheduler.Register(jobs.ScheduledTask{Name: "no-handler", Interval: time.Hour, LockKey: 1}); !errors.Is(err, jobs.ErrHandlerRequired) {
		t.Fatalf("Register without a handler = %v, want ErrHandlerRequired", err)
	}
	if err := scheduler.Register(jobs.ScheduledTask{Name: "", Interval: time.Hour, LockKey: 1, Run: valid.Run}); !errors.Is(err, jobs.ErrTaskName) {
		t.Fatalf("Register without a name = %v, want ErrTaskName", err)
	}
	if _, err := scheduler.RunTask(context.Background(), "missing"); !errors.Is(err, jobs.ErrTaskUnknown) {
		t.Fatalf("RunTask of an unknown task = %v, want ErrTaskUnknown", err)
	}
}

func TestSchedulerRunsEveryTaskOnStartAndStopsWithTheContext(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	var runs atomic.Int32
	scheduler := jobs.NewScheduler(pool)
	if err := scheduler.Register(jobs.ScheduledTask{
		Name:     "startup",
		Interval: time.Hour,
		LockKey:  testLockKey + 4,
		Run: func(context.Context) error {
			if runs.Add(1) == 1 {
				close(done)
			}
			return nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- scheduler.Run(runCtx) }()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("the task did not run on start")
	}

	stop()
	select {
	case err := <-stopped:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop with the context")
	}
}

func TestAPanickingTaskBecomesAnErrorAndKeepsTheSchedulerRunning(t *testing.T) {
	pool := dbtest.New(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var runs atomic.Int32
	scheduler := jobs.NewScheduler(pool)
	if err := scheduler.Register(jobs.ScheduledTask{
		Name:     "panics",
		Interval: 50 * time.Millisecond,
		LockKey:  testLockKey + 5,
		Run: func(context.Context) error {
			runs.Add(1)
			panic("the scheduled task exploded")
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err := scheduler.RunTask(ctx, "panics")
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("RunTask error = %v, want a panic error", err)
	}

	ok, err := scheduler.RunTask(ctx, "panics")
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("second RunTask error = %v, want a panic error", err)
	}
	if !ok {
		t.Fatal("the leader lock was not released after a panicking task")
	}

	runCtx, stop := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- scheduler.Run(runCtx) }()

	deadline := time.After(10 * time.Second)
	for runs.Load() < 4 {
		select {
		case <-deadline:
			t.Fatalf("the scheduler stopped ticking after a panic: %d runs", runs.Load())
		case <-time.After(20 * time.Millisecond):
		}
	}

	stop()
	select {
	case err := <-stopped:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not stop with the context")
	}
}

func TestATickIsSkippedWhenThePoolHasNoFreeConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := pgxpool.ParseConfig(dbtest.NewDatabaseURL(t))
	if err != nil {
		t.Fatalf("parse the test database url: %v", err)
	}
	config.MaxConns = 1

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("open a single connection pool: %v", err)
	}
	defer pool.Close()

	held, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("pin the only connection: %v", err)
	}
	released := false
	defer func() {
		if !released {
			held.Release()
		}
	}()

	var runs atomic.Int32
	scheduler := jobs.NewScheduler(pool, jobs.WithLockAcquireTimeout(200*time.Millisecond))
	if err := scheduler.Register(jobs.ScheduledTask{
		Name:     "starved",
		Interval: time.Hour,
		LockKey:  testLockKey + 6,
		Run: func(context.Context) error {
			runs.Add(1)
			return nil
		},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	started := time.Now()
	leader, err := scheduler.RunTask(ctx, "starved")
	if !errors.Is(err, jobs.ErrLockBusy) {
		t.Fatalf("RunTask error = %v, want ErrLockBusy", err)
	}
	if leader {
		t.Fatal("RunTask claimed the leader lock without a connection")
	}
	if waited := time.Since(started); waited > 5*time.Second {
		t.Fatalf("RunTask blocked for %s instead of skipping the tick", waited)
	}
	if got := runs.Load(); got != 0 {
		t.Fatalf("the task ran %d times without a connection", got)
	}

	held.Release()
	released = true

	leader, err = scheduler.RunTask(ctx, "starved")
	if err != nil {
		t.Fatalf("RunTask after the connection was freed: %v", err)
	}
	if !leader {
		t.Fatal("RunTask did not take the leader lock once a connection was free")
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("the task ran %d times, want 1", got)
	}
}
