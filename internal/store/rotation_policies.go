package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RotationPolicy schedules secret rotation for one key.
type RotationPolicy struct {
	ID         string
	ProjectID  string
	EnvID      string
	SecretKey  string
	Provider   string
	Config     []byte
	Interval   int
	Enabled    bool
	LastRunAt  *time.Time
	NextRunAt  time.Time
	LastStatus string
	LastError  string
	CreatedAt  time.Time
}

// RotationRun is one attempt at rotating a policy.
type RotationRun struct {
	ID         string
	PolicyID   string
	Status     string
	Phase      string
	Message    string
	StartedAt  time.Time
	FinishedAt *time.Time
}

func scanRotationPolicy(row rowScanner) (*RotationPolicy, error) {
	var policy RotationPolicy
	var lastRun, nextRun, createdAt sql.NullString
	var enabled int
	if err := row.Scan(&policy.ID, &policy.ProjectID, &policy.EnvID, &policy.SecretKey,
		&policy.Provider, &policy.Config, &policy.Interval, &enabled, &lastRun, &nextRun,
		&policy.LastStatus, &policy.LastError, &createdAt); err != nil {
		return nil, err
	}
	policy.Enabled = enabled != 0
	if lastRun.Valid {
		parsed, err := parseTime(lastRun.String)
		if err != nil {
			return nil, err
		}
		policy.LastRunAt = &parsed
	}
	var err error
	if policy.NextRunAt, err = parseTime(nextRun.String); err != nil {
		return nil, err
	}
	if policy.CreatedAt, err = parseTime(createdAt.String); err != nil {
		return nil, err
	}
	return &policy, nil
}

func (s *Store) CreateRotationPolicy(ctx context.Context, policy *RotationPolicy) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO rotation_policies
		(id, project_id, environment_id, secret_key, provider, config, interval_seconds,
		 enabled, last_run_at, next_run_at, last_status, last_error, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		policy.ID, policy.ProjectID, policy.EnvID, policy.SecretKey, policy.Provider,
		policy.Config, policy.Interval, boolInt(policy.Enabled), nullableTime(policy.LastRunAt),
		formatTime(policy.NextRunAt), policy.LastStatus, policy.LastError, formatTime(policy.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create rotation policy: %w", err)
	}
	return nil
}

func (s *Store) ListRotationPolicies(ctx context.Context, projectID, envID string) ([]RotationPolicy, error) {
	query := `SELECT id, project_id, environment_id, secret_key, provider, config, interval_seconds,
		enabled, last_run_at, next_run_at, last_status, last_error, created_at
		FROM rotation_policies WHERE project_id = ?`
	args := []any{projectID}
	if envID != "" {
		query += ` AND environment_id = ?`
		args = append(args, envID)
	}
	query += ` ORDER BY next_run_at`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list rotation policies: %w", err)
	}
	defer rows.Close()
	var out []RotationPolicy
	for rows.Next() {
		policy, err := scanRotationPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *policy)
	}
	return out, rows.Err()
}

func (s *Store) GetRotationPolicy(ctx context.Context, projectID, id string) (*RotationPolicy, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, environment_id, secret_key, provider, config,
		interval_seconds, enabled, last_run_at, next_run_at, last_status, last_error, created_at
		FROM rotation_policies WHERE project_id = ? AND id = ?`, projectID, id)
	policy, err := scanRotationPolicy(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get rotation policy: %w", err)
	}
	return policy, nil
}

func (s *Store) UpdateRotationPolicy(ctx context.Context, policy *RotationPolicy) error {
	res, err := s.db.ExecContext(ctx, `UPDATE rotation_policies SET interval_seconds = ?, enabled = ?,
		last_run_at = ?, next_run_at = ?, last_status = ?, last_error = ? WHERE id = ?`,
		policy.Interval, boolInt(policy.Enabled), nullableTime(policy.LastRunAt),
		formatTime(policy.NextRunAt), policy.LastStatus, policy.LastError, policy.ID)
	if err != nil {
		return err
	}
	return expectOne(res, "update rotation policy")
}

func (s *Store) DeleteRotationPolicy(ctx context.Context, projectID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM rotation_policies WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	return expectOne(res, "delete rotation policy")
}

func (s *Store) ListDueRotationPolicies(ctx context.Context, now time.Time) ([]RotationPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, environment_id, secret_key, provider, config,
		interval_seconds, enabled, last_run_at, next_run_at, last_status, last_error, created_at
		FROM rotation_policies WHERE enabled = 1 AND next_run_at <= ? ORDER BY next_run_at LIMIT 50`,
		formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: list due rotations: %w", err)
	}
	defer rows.Close()
	var out []RotationPolicy
	for rows.Next() {
		policy, err := scanRotationPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *policy)
	}
	return out, rows.Err()
}

func (s *Store) CreateRotationRun(ctx context.Context, run *RotationRun) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO rotation_runs
		(id, policy_id, status, phase, message, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?)`,
		run.ID, run.PolicyID, run.Status, run.Phase, run.Message,
		formatTime(run.StartedAt), nullableTime(run.FinishedAt))
	if err != nil {
		return fmt.Errorf("store: create rotation run: %w", err)
	}
	return nil
}

func (s *Store) FinishRotationRun(ctx context.Context, id, status, phase, message string, finished time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE rotation_runs SET status = ?, phase = ?, message = ?, finished_at = ? WHERE id = ?`,
		status, phase, message, formatTime(finished), id)
	if err != nil {
		return err
	}
	return expectOne(res, "finish rotation run")
}

func (s *Store) ListRotationRuns(ctx context.Context, policyID string, limit int) ([]RotationRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, policy_id, status, phase, message, started_at, finished_at
		FROM rotation_runs WHERE policy_id = ? ORDER BY started_at DESC LIMIT ?`, policyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RotationRun
	for rows.Next() {
		var run RotationRun
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&run.ID, &run.PolicyID, &run.Status, &run.Phase, &run.Message, &startedAt, &finishedAt); err != nil {
			return nil, err
		}
		var err error
		if run.StartedAt, err = parseTime(startedAt); err != nil {
			return nil, err
		}
		if finishedAt.Valid {
			parsed, err := parseTime(finishedAt.String)
			if err != nil {
				return nil, err
			}
			run.FinishedAt = &parsed
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
