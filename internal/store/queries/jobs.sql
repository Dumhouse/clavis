-- Queries used by internal/store/jobs.go.

-- name: EnqueueJob :exec
INSERT INTO jobs (
    id, kind, payload, status, run_at, attempts, max_attempts, created_at, updated_at
) VALUES (?,?,?,?,?,?,?,?,?);

-- name: GetJob :one
SELECT * FROM jobs WHERE id = ?1;

-- name: CompleteJob :exec
UPDATE jobs
SET status = 'done', locked_by = '', locked_until = NULL, last_error = '', updated_at = ?2
WHERE id = ?1;

-- name: ReapExpiredJobs :exec
UPDATE jobs
SET status = 'pending', locked_by = '', locked_until = NULL, updated_at = ?2
WHERE status = 'running' AND locked_until IS NOT NULL AND locked_until < ?1;
