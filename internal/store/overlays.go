package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Overlay is a per-user set of personal overrides for one environment.
type Overlay struct {
	ID            string
	EnvironmentID string
	Slug          string
	Name          string
	CreatedAt     time.Time
}

// GetOrCreateOverlay returns the user's overlay in an environment.
func (s *Store) GetOrCreateOverlay(ctx context.Context, envID, userID, name string) (*Overlay, error) {
	overlay, err := s.GetOverlay(ctx, envID, userID)
	if err == nil {
		return overlay, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	overlay = &Overlay{
		ID: uuid.NewString(), EnvironmentID: envID, Slug: userID, Name: name,
		CreatedAt: time.Now().UTC(),
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO overlays (id, environment_id, slug, name, created_at)
		VALUES (?,?,?,?,?)`, overlay.ID, overlay.EnvironmentID, overlay.Slug, overlay.Name,
		formatTime(overlay.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return s.GetOverlay(ctx, envID, userID)
		}
		return nil, fmt.Errorf("store: create overlay: %w", err)
	}
	return overlay, nil
}

// GetOverlay loads a user's overlay in an environment.
func (s *Store) GetOverlay(ctx context.Context, envID, userID string) (*Overlay, error) {
	var overlay Overlay
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, environment_id, slug, name, created_at
		FROM overlays WHERE environment_id = ? AND slug = ?`, envID, userID).
		Scan(&overlay.ID, &overlay.EnvironmentID, &overlay.Slug, &overlay.Name, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get overlay: %w", err)
	}
	if overlay.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &overlay, nil
}

// ListOverlayItems returns the live items in an overlay.
func (s *Store) ListOverlayItems(ctx context.Context, overlayID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColumns+` FROM items
		WHERE overlay_id = ? AND deleted_at IS NULL ORDER BY updated_at DESC`, overlayID)
	if err != nil {
		return nil, fmt.Errorf("store: list overlay items: %w", err)
	}
	defer rows.Close()
	var out []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetOverlayItemByBlindIndex loads one live overlay item.
func (s *Store) GetOverlayItemByBlindIndex(ctx context.Context, overlayID, blind string) (*Item, error) {
	return scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items
		WHERE overlay_id = ? AND deleted_at IS NULL AND key_blind_index = ?`, overlayID, blind))
}
