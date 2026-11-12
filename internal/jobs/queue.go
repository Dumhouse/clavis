// Package jobs runs background work from the database-backed queue in the same
// process as the API server.
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"runtime/debug"
	"sync"
	"time"

	"github.com/dumhouse/clavis/internal/store"
)

// Handler processes one job payload. It must be idempotent where possible,
// because a lease can expire and the job can be retried.
type Handler func(ctx context.Context, payload []byte) error

// Queue claims and dispatches jobs.
type Queue struct {
	store        *store.Store
	logger       *slog.Logger
	workerID     string
	lease        time.Duration
	pollInterval time.Duration
	baseBackoff  time.Duration
	maxBackoff   time.Duration
	reapEvery    time.Duration

	mu       sync.RWMutex
	handlers map[string]Handler
}

// Option configures a Queue.
type Option func(*Queue)

// WithWorkerID sets the worker identity recorded on leased jobs.
func WithWorkerID(id string) Option { return func(q *Queue) { q.workerID = id } }

// WithLease sets how long a claimed job is leased before another worker may
// reclaim it.
func WithLease(d time.Duration) Option { return func(q *Queue) { q.lease = d } }

// WithPollInterval sets how often an idle worker looks for jobs.
func WithPollInterval(d time.Duration) Option { return func(q *Queue) { q.pollInterval = d } }

// WithBackoff sets the base and maximum retry delay.
func WithBackoff(base, max time.Duration) Option {
	return func(q *Queue) { q.baseBackoff = base; q.maxBackoff = max }
}

// New returns a queue with sane defaults.
func New(s *store.Store, logger *slog.Logger, opts ...Option) *Queue {
	if logger == nil {
		logger = slog.Default()
	}
	q := &Queue{
		store:        s,
		logger:       logger,
		workerID:     fmt.Sprintf("worker-%d", time.Now().UnixNano()),
		lease:        5 * time.Minute,
		pollInterval: 2 * time.Second,
		baseBackoff:  time.Second,
		maxBackoff:   10 * time.Minute,
		reapEvery:    time.Minute,
		handlers:     make(map[string]Handler),
	}
	for _, opt := range opts {
		opt(q)
	}
	return q
}

// Register adds a handler for a job kind. Registering twice panics: that is a
// programming error caught at startup.
func (q *Queue) Register(kind string, handler Handler) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if kind == "" || handler == nil {
		panic("jobs: kind and handler are required")
	}
	if _, ok := q.handlers[kind]; ok {
		panic("jobs: duplicate handler for " + kind)
	}
	q.handlers[kind] = handler
}

func (q *Queue) handler(kind string) (Handler, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	h, ok := q.handlers[kind]
	return h, ok
}

// ProcessOne claims and runs at most one job. It reports whether a job was
// claimed.
func (q *Queue) ProcessOne(ctx context.Context) (bool, error) {
	job, err := q.store.ClaimJob(ctx, q.workerID, q.lease)
	if err != nil {
		return false, err
	}
	if job == nil {
		return false, nil
	}

	handler, ok := q.handler(job.Kind)
	if !ok {
		err := fmt.Errorf("jobs: no handler registered for kind %q", job.Kind)
		q.logger.Error("job has no handler", "job_id", job.ID, "kind", job.Kind)
		return true, q.fail(ctx, job, err)
	}

	log := q.logger.With("job_id", job.ID, "kind", job.Kind, "attempt", job.Attempts)
	if err := q.runHandler(ctx, handler, job.Payload); err != nil {
		log.Error("job failed", "error", err)
		return true, q.fail(ctx, job, err)
	}
	if err := q.store.CompleteJob(ctx, job.ID); err != nil {
		return true, err
	}
	log.Info("job completed")
	return true, nil
}

func (q *Queue) runHandler(ctx context.Context, handler Handler, payload []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			// Never include the panic value: it may contain secret material.
			q.logger.Error("job handler panicked", "stack", string(debug.Stack()))
			err = errors.New("jobs: handler panicked")
		}
	}()
	return handler(ctx, payload)
}

func (q *Queue) fail(ctx context.Context, job *store.Job, cause error) error {
	delay := jitter(Backoff(job.Attempts, q.baseBackoff, q.maxBackoff))
	if err := q.store.FailJob(ctx, job.ID, cause.Error(), time.Now().Add(delay)); err != nil {
		return errors.Join(cause, err)
	}
	return nil
}

// Run processes jobs until ctx is cancelled, reclaiming expired leases
// periodically.
func (q *Queue) Run(ctx context.Context) error {
	q.logger.Info("job worker started", "worker_id", q.workerID)
	ticker := time.NewTicker(q.pollInterval)
	defer ticker.Stop()
	reaper := time.NewTicker(q.reapEvery)
	defer reaper.Stop()

	for {
		for {
			claimed, err := q.ProcessOne(ctx)
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return nil
				}
				q.logger.Error("job processing error", "error", err)
			}
			if !claimed {
				break
			}
		}
		select {
		case <-ctx.Done():
			q.logger.Info("job worker stopped")
			return nil
		case <-ticker.C:
		case now := <-reaper.C:
			if n, err := q.store.ReapExpiredJobs(ctx, now); err != nil {
				q.logger.Error("reap jobs failed", "error", err)
			} else if n > 0 {
				q.logger.Warn("reclaimed expired job leases", "count", n)
			}
		}
	}
}

// Backoff returns the deterministic exponential delay for an attempt (1-based)
// capped at max. Attempts beyond 62 saturate to max.
func Backoff(attempt int, base, max time.Duration) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if base <= 0 {
		base = time.Second
	}
	if max <= 0 {
		max = 10 * time.Minute
	}
	if attempt > 62 {
		return max
	}
	delay := base << (attempt - 1)
	if delay <= 0 || delay > max {
		return max
	}
	return delay
}

// jitter adds up to 20% positive jitter so retries do not synchronize.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return d
	}
	return d + time.Duration(rand.Int64N(int64(d)/5+1))
}
