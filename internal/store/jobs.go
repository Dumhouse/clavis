package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Job statuses.
const (
	JobPending = "pending"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// Job is a row in the built-in database-backed queue.
type Job struct {
	ID          string
	Kind        string
	Payload     []byte
	Status      string
	RunAt       time.Time
	Attempts    int
	MaxAttempts int
	LockedBy    string
	LockedUntil *time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// EnqueueJob adds a job that becomes runnable at runAt.
func (s *Store) EnqueueJob(ctx context.Context, kind string, payload []byte, runAt time.Time) (*Job, error) {
	if kind == "" {
		return nil, fmt.Errorf("store: enqueue job: kind is required")
	}
	now := time.Now().UTC()
	if payload == nil {
		payload = []byte("{}")
	}
	job := &Job{
		ID:          uuid.NewString(),
		Kind:        kind,
		Payload:     payload,
		Status:      JobPending,
		RunAt:       runAt.UTC(),
		MaxAttempts: 10,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO jobs (
		id, kind, payload, status, run_at, attempts, max_attempts, created_at, updated_at
	) VALUES (?,?,?,?,?,?,?,?,?)`,
		job.ID, job.Kind, job.Payload, job.Status, formatTime(job.RunAt),
		job.Attempts, job.MaxAttempts, formatTime(job.CreatedAt), formatTime(job.UpdatedAt))
	if err != nil {
		return nil, fmt.Errorf("store: enqueue job: %w", err)
	}
	return job, nil
}

const jobColumns = `id, kind, payload, status, run_at, attempts, max_attempts,
	locked_by, locked_until, last_error, created_at, updated_at`

func scanJob(row rowScanner) (*Job, error) {
	var (
		job                         Job
		runAt, createdAt, updatedAt string
		lockedUntil                 sql.NullString
	)
	err := row.Scan(
		&job.ID, &job.Kind, &job.Payload, &job.Status, &runAt, &job.Attempts,
		&job.MaxAttempts, &job.LockedBy, &lockedUntil, &job.LastError,
		&createdAt, &updatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan job: %w", err)
	}
	var parseErr error
	if job.RunAt, parseErr = parseTime(runAt); parseErr != nil {
		return nil, parseErr
	}
	if job.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return nil, parseErr
	}
	if job.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return nil, parseErr
	}
	if job.LockedUntil, parseErr = parseNullableTime(lockedUntil); parseErr != nil {
		return nil, parseErr
	}
	return &job, nil
}

// ClaimJob atomically leases the next due job to worker. It returns nil when
// no job is due. The UPDATE ... RETURNING form makes the claim a single atomic
// statement, which is safe for multiple workers.
func (s *Store) ClaimJob(ctx context.Context, worker string, lease time.Duration) (*Job, error) {
	if worker == "" {
		return nil, fmt.Errorf("store: claim job: worker is required")
	}
	now := time.Now().UTC()
	skipLocked := ""
	if s.dialect == "postgres" {
		skipLocked = " FOR UPDATE SKIP LOCKED"
	}
	job, err := scanJob(s.db.QueryRowContext(ctx, `UPDATE jobs
		SET status = 'running',
		    locked_by = ?,
		    locked_until = ?,
		    attempts = attempts + 1,
		    updated_at = ?
		WHERE id = (
			SELECT id FROM jobs
			WHERE status IN ('pending','running')
			  AND run_at <= ?
			  AND (locked_until IS NULL OR locked_until < ?)
			ORDER BY run_at ASC, created_at ASC
			LIMIT 1`+skipLocked+`
		)
		RETURNING `+jobColumns,
		worker, formatTime(now.Add(lease)), formatTime(now), formatTime(now), formatTime(now)))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	return job, err
}

// CompleteJob marks a job as done.
func (s *Store) CompleteJob(ctx context.Context, id string) error {
	now := formatTime(time.Now())
	res, err := s.db.ExecContext(ctx, `UPDATE jobs
		SET status = 'done', locked_by = '', locked_until = NULL, last_error = '', updated_at = ?
		WHERE id = ?`, now, id)
	if err != nil {
		return fmt.Errorf("store: complete job: %w", err)
	}
	return expectOne(res, "complete job")
}

// FailJob records a failed attempt. If attempts are exhausted the job moves to
// failed; otherwise it returns to pending at retryAt.
func (s *Store) FailJob(ctx context.Context, id, cause string, retryAt time.Time) error {
	now := formatTime(time.Now())
	res, err := s.db.ExecContext(ctx, `UPDATE jobs
		SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
		    run_at = ?,
		    locked_by = '',
		    locked_until = NULL,
		    last_error = ?,
		    updated_at = ?
		WHERE id = ?`, formatTime(retryAt), cause, now, id)
	if err != nil {
		return fmt.Errorf("store: fail job: %w", err)
	}
	return expectOne(res, "fail job")
}

// ReapExpiredJobs returns jobs whose lease expired to pending. It returns the
// number of jobs reclaimed.
func (s *Store) ReapExpiredJobs(ctx context.Context, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE jobs
		SET status = 'pending', locked_by = '', locked_until = NULL, updated_at = ?
		WHERE status = 'running' AND locked_until IS NOT NULL AND locked_until < ?`,
		formatTime(now), formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("store: reap jobs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("store: reap jobs rows: %w", err)
	}
	return n, nil
}

// GetJob loads a job by id.
func (s *Store) GetJob(ctx context.Context, id string) (*Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = ?`, id))
}

// ListJobs returns jobs filtered by status (empty means all), newest first.
func (s *Store) ListJobs(ctx context.Context, status string, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `SELECT ` + jobColumns + ` FROM jobs`
	args := []any{}
	if status != "" {
		query += ` WHERE status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list jobs: %w", err)
	}
	defer rows.Close()
	var jobs []Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, *job)
	}
	return jobs, rows.Err()
}

func expectOne(res sql.Result, op string) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: %s rows: %w", op, err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
