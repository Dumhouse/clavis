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
