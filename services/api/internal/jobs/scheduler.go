package jobs

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const DefaultScheduleInterval = time.Hour

var (
	ErrTaskRegistered = errors.New("jobs: scheduled task is already registered")
	ErrTaskName       = errors.New("jobs: a scheduled task needs a name")
	ErrTaskUnknown    = errors.New("jobs: scheduled task is not registered")
	ErrNoTasks        = errors.New("jobs: no scheduled tasks registered")
	ErrSchedulerBusy  = errors.New("jobs: scheduler is already running")
)

type ScheduledTask struct {
	Name      string
	Interval  time.Duration
	LockKey   int64
	Run       func(ctx context.Context) error
	SkipFirst bool
}

type Scheduler struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger

	mu      sync.Mutex
	order   []string
	tasks   map[string]ScheduledTask
	started bool
}

type SchedulerOption func(*Scheduler)

func WithSchedulerLogger(logger zerolog.Logger) SchedulerOption {
	return func(s *Scheduler) {
		s.logger = logger
	}
}

func NewScheduler(pool *pgxpool.Pool, options ...SchedulerOption) *Scheduler {
	scheduler := &Scheduler{pool: pool, tasks: map[string]ScheduledTask{}}
	for _, apply := range options {
		apply(scheduler)
	}
	return scheduler
}

func (s *Scheduler) Register(task ScheduledTask) error {
	name := strings.TrimSpace(task.Name)
	if name == "" {
		return ErrTaskName
	}
	if task.Run == nil {
		return fmt.Errorf("%w: %s", ErrHandlerRequired, name)
	}
	if task.Interval <= 0 {
		task.Interval = DefaultScheduleInterval
	}
	task.Name = name

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return ErrSchedulerBusy
	}
	if _, exists := s.tasks[name]; exists {
		return fmt.Errorf("%w: %s", ErrTaskRegistered, name)
	}
	s.tasks[name] = task
	s.order = append(s.order, name)
	return nil
}

func (s *Scheduler) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrSchedulerBusy
	}
	if len(s.order) == 0 {
		s.mu.Unlock()
		return ErrNoTasks
	}
	s.started = true
	names := append([]string(nil), s.order...)
	s.mu.Unlock()

	var wait sync.WaitGroup
	for _, name := range names {
		task := s.task(name)
		wait.Add(1)
		go func() {
			defer wait.Done()
			s.loop(ctx, task)
		}()
	}
	wait.Wait()

	s.mu.Lock()
	s.started = false
	s.mu.Unlock()

	if err := ctx.Err(); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

func (s *Scheduler) loop(ctx context.Context, task ScheduledTask) {
	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	if !task.SkipFirst {
		s.tick(ctx, task)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx, task)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context, task ScheduledTask) {
	leader, err := s.runWithLock(ctx, task)
	switch {
	case err != nil && errors.Is(err, context.Canceled):
		return
	case err != nil:
		s.logger.Error().Err(err).Str("task", task.Name).Msg("scheduled task failed")
	case !leader:
		s.logger.Debug().Str("task", task.Name).Msg("another instance holds the scheduler lock")
	}
}

func (s *Scheduler) RunTask(ctx context.Context, name string) (bool, error) {
	trimmed := strings.TrimSpace(name)
	s.mu.Lock()
	task, ok := s.tasks[trimmed]
	s.mu.Unlock()
	if !ok {
		return false, fmt.Errorf("%w: %s", ErrTaskUnknown, trimmed)
	}
	return s.runWithLock(ctx, task)
}

func (s *Scheduler) runWithLock(ctx context.Context, task ScheduledTask) (bool, error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire a connection for the %s lock: %w", task.Name, err)
	}
	defer conn.Release()

	var acquired bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock($1)`, task.LockKey).Scan(&acquired); err != nil {
		return false, fmt.Errorf("take the %s leader lock: %w", task.Name, err)
	}
	if !acquired {
		return false, nil
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(releaseCtx, `SELECT pg_advisory_unlock($1)`, task.LockKey); err != nil {
			s.logger.Error().Err(err).Str("task", task.Name).Msg("releasing the scheduler lock failed")
		}
	}()

	if err := task.Run(ctx); err != nil {
		return true, err
	}
	return true, nil
}

func (s *Scheduler) task(name string) ScheduledTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tasks[name]
}
