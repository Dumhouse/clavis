package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Sync pushes an environment's resolved secrets to an external provider.
type Sync struct {
	ID             string
	ProjectID      string
	EnvironmentID  string
	Path           string
	Provider       string
	Config         string
	Status         string
	Confirmed      bool
	DriftKeys      string
	DriftCheckedAt *time.Time
	LastSyncedAt   *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// SyncRun is one execution attempt.
type SyncRun struct {
	ID          string
	SyncID      string
	ProjectID   string
	Status      string
	DryRun      bool
	SecretCount int
	Error       string
	Detail      string
	StartedAt   time.Time
	FinishedAt  *time.Time
}

// CreateSync inserts a sync definition.
func (s *Store) CreateSync(ctx context.Context, sync *Sync) error {
	if sync.ID == "" {
		sync.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if sync.CreatedAt.IsZero() {
		sync.CreatedAt = now
	}
	sync.UpdatedAt = now
	if sync.Path == "" {
		sync.Path = "/"
	}
	if sync.Status == "" {
		sync.Status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO syncs
		(id, project_id, environment_id, path, provider, config, status, confirmed, last_synced_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		sync.ID, sync.ProjectID, sync.EnvironmentID, sync.Path, sync.Provider, sync.Config,
		sync.Status, sync.Confirmed, nullableTime(sync.LastSyncedAt), formatTime(sync.CreatedAt), formatTime(sync.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: create sync: %w", err)
	}
	return nil
}

const syncColumns = `id, project_id, environment_id, path, provider, config, status,
	confirmed, drift_keys, drift_checked_at, last_synced_at, created_at, updated_at`

func scanSync(row rowScanner) (*Sync, error) {
	var sync Sync
	var confirmed int
	var lastSynced, driftChecked sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&sync.ID, &sync.ProjectID, &sync.EnvironmentID, &sync.Path, &sync.Provider,
		&sync.Config, &sync.Status, &confirmed, &sync.DriftKeys, &driftChecked,
		&lastSynced, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan sync: %w", err)
	}
	sync.Confirmed = confirmed != 0
	var err error
	if sync.DriftCheckedAt, err = parseNullableTime(driftChecked); err != nil {
		return nil, err
	}
	if sync.LastSyncedAt, err = parseNullableTime(lastSynced); err != nil {
		return nil, err
	}
	if sync.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if sync.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &sync, nil
}

// ListSyncs returns a project's syncs.
func (s *Store) ListSyncs(ctx context.Context, projectID string) ([]Sync, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+syncColumns+` FROM syncs
		WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list syncs: %w", err)
	}
	defer rows.Close()
	var out []Sync
	for rows.Next() {
		sync, err := scanSync(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sync)
	}
	return out, rows.Err()
}

// GetSync loads one sync in a project.
func (s *Store) GetSync(ctx context.Context, projectID, id string) (*Sync, error) {
	return scanSync(s.db.QueryRowContext(ctx, `SELECT `+syncColumns+` FROM syncs
		WHERE id = ? AND project_id = ?`, id, projectID))
}

// UpdateSyncStatus records a completed run.
func (s *Store) UpdateSyncStatus(ctx context.Context, id, status string, syncedAt *time.Time, confirmed bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE syncs
		SET status = ?, last_synced_at = ?, confirmed = ?, updated_at = ? WHERE id = ?`,
		status, nullableTime(syncedAt), confirmed, formatTime(time.Now().UTC()), id)
	if err != nil {
		return fmt.Errorf("store: update sync: %w", err)
	}
	return expectOne(res, "update sync")
}

// UpdateSyncConfig replaces the provider configuration JSON.
func (s *Store) UpdateSyncConfig(ctx context.Context, projectID, id, config string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE syncs SET config = ?, updated_at = ?
		WHERE id = ? AND project_id = ?`, config, formatTime(time.Now().UTC()), id, projectID)
	if err != nil {
		return fmt.Errorf("store: update sync config: %w", err)
	}
	return expectOne(res, "update sync config")
}

// UpdateSyncDrift records a drift check result.
func (s *Store) UpdateSyncDrift(ctx context.Context, id, keys string, checkedAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE syncs SET drift_keys = ?, drift_checked_at = ? WHERE id = ?`,
		keys, formatTime(checkedAt), id)
	if err != nil {
		return fmt.Errorf("store: update sync drift: %w", err)
	}
	return expectOne(res, "update sync drift")
}

// DeleteSync removes a sync and its runs.
func (s *Store) DeleteSync(ctx context.Context, projectID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM syncs WHERE id = ? AND project_id = ?`, id, projectID)
	if err != nil {
		return fmt.Errorf("store: delete sync: %w", err)
	}
	return expectOne(res, "delete sync")
}

// CreateSyncRun inserts a run.
func (s *Store) CreateSyncRun(ctx context.Context, run *SyncRun) error {
	if run.ID == "" {
		run.ID = uuid.NewString()
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if run.Status == "" {
		run.Status = "pending"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_runs
		(id, sync_id, project_id, status, dry_run, secret_count, error, detail, started_at, finished_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		run.ID, run.SyncID, run.ProjectID, run.Status, run.DryRun, run.SecretCount,
		run.Error, run.Detail, formatTime(run.StartedAt), nullableTime(run.FinishedAt))
	if err != nil {
		return fmt.Errorf("store: create sync run: %w", err)
	}
	return nil
}

// UpdateSyncRun completes a run.
func (s *Store) UpdateSyncRun(ctx context.Context, run *SyncRun) error {
	res, err := s.db.ExecContext(ctx, `UPDATE sync_runs
		SET status = ?, secret_count = ?, error = ?, detail = ?, finished_at = ? WHERE id = ?`,
		run.Status, run.SecretCount, run.Error, run.Detail, nullableTime(run.FinishedAt), run.ID)
	if err != nil {
		return fmt.Errorf("store: update sync run: %w", err)
	}
	return expectOne(res, "update sync run")
}

// ListSyncRuns returns recent runs, newest first.
func (s *Store) ListSyncRuns(ctx context.Context, syncID string, limit int) ([]SyncRun, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, sync_id, project_id, status, dry_run, secret_count,
			error, detail, started_at, finished_at
		FROM sync_runs WHERE sync_id = ? ORDER BY started_at DESC LIMIT ?`, syncID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list sync runs: %w", err)
	}
	defer rows.Close()
	var out []SyncRun
	for rows.Next() {
		var run SyncRun
		var dryRun int
		var startedAt string
		var finishedAt sql.NullString
		if err := rows.Scan(&run.ID, &run.SyncID, &run.ProjectID, &run.Status, &dryRun,
			&run.SecretCount, &run.Error, &run.Detail, &startedAt, &finishedAt); err != nil {
			return nil, fmt.Errorf("store: scan sync run: %w", err)
		}
		run.DryRun = dryRun != 0
		if run.StartedAt, err = parseTime(startedAt); err != nil {
			return nil, err
		}
		if run.FinishedAt, err = parseNullableTime(finishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
