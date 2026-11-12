package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Snapshot is an encrypted point-in-time capture of an environment.
type Snapshot struct {
	ID        string
	EnvID     string
	Reason    string
	Payload   []byte
	CreatedAt time.Time
}

// CreateSnapshot inserts a snapshot.
func (s *Store) CreateSnapshot(ctx context.Context, snapshot *Snapshot) error {
	if snapshot.ID == "" {
		snapshot.ID = uuid.NewString()
	}
	if snapshot.CreatedAt.IsZero() {
		snapshot.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO snapshots (id, environment_id, reason, payload, created_at)
		VALUES (?,?,?,?,?)`,
		snapshot.ID, snapshot.EnvID, snapshot.Reason, snapshot.Payload, formatTime(snapshot.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create snapshot: %w", err)
	}
	return nil
}

// ListSnapshots returns an environment's snapshots, newest first.
func (s *Store) ListSnapshots(ctx context.Context, envID string, limit int) ([]Snapshot, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, environment_id, reason, created_at
		FROM snapshots WHERE environment_id = ? ORDER BY created_at DESC LIMIT ?`, envID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Snapshot
	for rows.Next() {
		var snapshot Snapshot
		var createdAt string
		if err := rows.Scan(&snapshot.ID, &snapshot.EnvID, &snapshot.Reason, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan snapshot: %w", err)
		}
		if snapshot.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, snapshot)
	}
	return out, rows.Err()
}

// GetSnapshot loads a snapshot payload in an org/project scope.
func (s *Store) GetSnapshot(ctx context.Context, envID, id string) (*Snapshot, error) {
	var snapshot Snapshot
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, environment_id, reason, payload, created_at
		FROM snapshots WHERE id = ? AND environment_id = ?`, id, envID).
		Scan(&snapshot.ID, &snapshot.EnvID, &snapshot.Reason, &snapshot.Payload, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get snapshot: %w", err)
	}
	if snapshot.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &snapshot, nil
}
