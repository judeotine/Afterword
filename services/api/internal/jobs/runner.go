package jobs

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	DefaultPollInterval  = time.Second
	DefaultJitter        = 0.3
	DefaultStaleAfter    = 5 * time.Minute
	DefaultSweepInterval = time.Minute
)

var (
	ErrNoHandlers      = errors.New("jobs: no handlers registered")
	ErrKindRegistered  = errors.New("jobs: kind already registered")
	ErrHandlerRequired = errors.New("jobs: handler is required")
	ErrConcurrency     = errors.New("jobs: concurrency must be positive")
	ErrRunning         = errors.New("jobs: runner is already running")
)

type Handler func(ctx context.Context, job *Job) error

type registration struct {
	handler     Handler
	concurrency int
}

type Runner struct {
	queue *Queue

	workerID       string
	pollInterval   time.Duration
	jitter         float64
	staleAfter     time.Duration
	sweepInterval  time.Duration
	handlerTimeout time.Duration
	logger         zerolog.Logger

	mu       sync.Mutex
	handlers map[string]registration
	started  bool
}

type RunnerOption func(*Runner)

func WithWorkerID(workerID string) RunnerOption {
	return func(r *Runner) {
		if strings.TrimSpace(workerID) != "" {
			r.workerID = workerID
		}
	}
}

func WithPollInterval(interval time.Duration) RunnerOption {
	return func(r *Runner) {
		if interval > 0 {
			r.pollInterval = interval
		}
	}
}

func WithJitter(jitter float64) RunnerOption {
	return func(r *Runner) {
		if jitter >= 0 && jitter <= 1 {
			r.jitter = jitter
		}
	}
}

func WithStaleAfter(staleAfter time.Duration) RunnerOption {
	return func(r *Runner) {
		if staleAfter > 0 {
			r.staleAfter = staleAfter
		}
	}
}

func WithSweepInterval(interval time.Duration) RunnerOption {
	return func(r *Runner) {
		if interval > 0 {
			r.sweepInterval = interval
		}
	}
}

func WithHandlerTimeout(timeout time.Duration) RunnerOption {
	return func(r *Runner) {
		if timeout > 0 {
			r.handlerTimeout = timeout
		}
	}
}

func WithLogger(logger zerolog.Logger) RunnerOption {
	return func(r *Runner) { r.logger = logger }
}

func NewRunner(queue *Queue, opts ...RunnerOption) *Runner {
	runner := &Runner{
		queue:         queue,
		workerID:      defaultWorkerID(),
		pollInterval:  DefaultPollInterval,
		jitter:        DefaultJitter,
		staleAfter:    DefaultStaleAfter,
		sweepInterval: DefaultSweepInterval,
		logger:        zerolog.Nop(),
		handlers:      map[string]registration{},
	}
	for _, opt := range opts {
		opt(runner)
	}
	return runner
}

func (r *Runner) Register(kind string, concurrency int, handler Handler) error {
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return ErrKindRequired
	}
	if handler == nil {
		return ErrHandlerRequired
	}
	if concurrency < 1 {
		return ErrConcurrency
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started {
		return ErrRunning
	}
	if _, exists := r.handlers[kind]; exists {
		return fmt.Errorf("%w: %s", ErrKindRegistered, kind)
	}
	r.handlers[kind] = registration{handler: handler, concurrency: concurrency}
	return nil
}

func (r *Runner) Run(ctx context.Context) error {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return ErrRunning
	}
	if len(r.handlers) == 0 {
		r.mu.Unlock()
		return ErrNoHandlers
	}
	snapshot := make(map[string]registration, len(r.handlers))
	for kind, reg := range r.handlers {
		snapshot[kind] = reg
	}
	r.started = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.started = false
		r.mu.Unlock()
	}()

	var wg sync.WaitGroup
	for kind, reg := range snapshot {
		for slot := 0; slot < reg.concurrency; slot++ {
			wg.Add(1)
			go func(kind string, reg registration, slot int) {
				defer wg.Done()
				r.work(ctx, fmt.Sprintf("%s-%s-%d", r.workerID, kind, slot), kind, reg.handler)
			}(kind, reg, slot)
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		r.sweep(ctx)
	}()

	wg.Wait()
	return ctx.Err()
}

func (r *Runner) work(ctx context.Context, workerID string, kind string, handler Handler) {
	kinds := []string{kind}
	for {
		if ctx.Err() != nil {
			return
		}

		job, err := r.queue.Claim(ctx, workerID, kinds)
		switch {
		case errors.Is(err, ErrNoJob):
			if !r.wait(ctx) {
				return
			}
			continue
		case err != nil:
			if ctx.Err() != nil {
				return
			}
			r.logger.Error().Err(err).Str("kind", kind).Str("worker", workerID).Msg("claim job failed")
			if !r.wait(ctx) {
				return
			}
			continue
		}

		r.dispatch(ctx, workerID, job, handler)
	}
}

func (r *Runner) dispatch(ctx context.Context, workerID string, job *Job, handler Handler) {
	handlerErr := r.invoke(ctx, job, handler)

	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()

	if handlerErr != nil {
		r.logger.Warn().Err(handlerErr).
			Str("kind", job.Kind).Str("worker", workerID).Str("job", job.ID.String()).
			Int("attempt", int(job.Attempts)).Msg("job handler failed")
		if _, err := r.queue.Fail(finishCtx, job.ID, workerID, handlerErr); err != nil {
			r.logger.Error().Err(err).Str("job", job.ID.String()).Msg("record job failure")
		}
		return
	}

	if _, err := r.queue.Complete(finishCtx, job.ID, workerID); err != nil {
		r.logger.Error().Err(err).Str("job", job.ID.String()).Msg("record job completion")
	}
}

func (r *Runner) invoke(ctx context.Context, job *Job, handler Handler) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("jobs: handler panicked: %v", recovered)
		}
	}()

	if r.handlerTimeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, r.handlerTimeout)
		defer cancel()
		return handler(timeoutCtx, job)
	}
	return handler(ctx, job)
}

func (r *Runner) sweep(ctx context.Context) {
	ticker := time.NewTicker(r.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			released, err := r.queue.ReleaseStale(ctx, r.staleAfter)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				r.logger.Error().Err(err).Msg("release stale jobs failed")
				continue
			}
			if released > 0 {
				r.logger.Info().Int64("released", released).Msg("released stale jobs")
			}
		}
	}
}

func (r *Runner) wait(ctx context.Context) bool {
	timer := time.NewTimer(r.nextInterval())
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runner) nextInterval() time.Duration {
	if r.jitter <= 0 {
		return r.pollInterval
	}
	spread := float64(r.pollInterval) * r.jitter
	offset := (rand.Float64()*2 - 1) * spread
	interval := time.Duration(float64(r.pollInterval) + offset)
	if interval < time.Millisecond {
		return time.Millisecond
	}
	return interval
}

func defaultWorkerID() string {
	return fmt.Sprintf("worker-%d", rand.Uint64())
}
