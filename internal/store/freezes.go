package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ActiveFreeze returns the active freeze window for an environment, if any.
func (s *Store) ActiveFreeze(ctx context.Context, envID string, at time.Time) (*FreezeWindow, error) {
	var freeze FreezeWindow
	var startsAt, endsAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, environment_id, starts_at, ends_at, reason, created_by
		FROM freeze_windows
		WHERE environment_id = ? AND starts_at <= ? AND ends_at >= ?
		ORDER BY starts_at DESC LIMIT 1`, envID, formatTime(at), formatTime(at)).
		Scan(&freeze.ID, &freeze.EnvironmentID, &startsAt, &endsAt, &freeze.Reason, &freeze.CreatedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: active freeze: %w", err)
	}
	var err2 error
	if freeze.StartsAt, err2 = parseTime(startsAt); err2 != nil {
		return nil, err2
	}
	if freeze.EndsAt, err2 = parseTime(endsAt); err2 != nil {
		return nil, err2
	}
	return &freeze, nil
}

// FreezeWindow blocks changes to an environment.
type FreezeWindow struct {
	ID            string
	EnvironmentID string
	StartsAt      time.Time
	EndsAt        time.Time
	Reason        string
	CreatedBy     string
}

// CreateFreezeWindow inserts a scheduled freeze.
func (s *Store) CreateFreezeWindow(ctx context.Context, freeze *FreezeWindow) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO freeze_windows
		(id, environment_id, starts_at, ends_at, reason, created_by, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		freeze.ID, freeze.EnvironmentID, formatTime(freeze.StartsAt), formatTime(freeze.EndsAt),
		freeze.Reason, freeze.CreatedBy, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("store: create freeze: %w", err)
	}
	return nil
}

// ListFreezeWindows returns upcoming and active freezes for an environment.
func (s *Store) ListFreezeWindows(ctx context.Context, envID string, at time.Time) ([]FreezeWindow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, environment_id, starts_at, ends_at, reason, created_by
		FROM freeze_windows WHERE environment_id = ? AND ends_at >= ? ORDER BY starts_at`, envID, formatTime(at))
	if err != nil {
		return nil, fmt.Errorf("store: list freezes: %w", err)
	}
	defer rows.Close()
	var out []FreezeWindow
	for rows.Next() {
		var freeze FreezeWindow
		var startsAt, endsAt string
		if err := rows.Scan(&freeze.ID, &freeze.EnvironmentID, &startsAt, &endsAt,
			&freeze.Reason, &freeze.CreatedBy); err != nil {
			return nil, err
		}
		var err error
		if freeze.StartsAt, err = parseTime(startsAt); err != nil {
			return nil, err
		}
		if freeze.EndsAt, err = parseTime(endsAt); err != nil {
			return nil, err
		}
		out = append(out, freeze)
	}
	return out, rows.Err()
}

// DeleteFreezeWindow removes a scheduled freeze.
func (s *Store) DeleteFreezeWindow(ctx context.Context, envID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM freeze_windows WHERE environment_id = ? AND id = ?`, envID, id)
	if err != nil {
		return err
	}
	return expectOne(res, "delete freeze")
}
