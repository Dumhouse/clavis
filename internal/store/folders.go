package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Folder groups items by path inside an environment.
type Folder struct {
	ID            string
	EnvironmentID string
	Path          string
	CreatedAt     time.Time
}

// CreateFolder inserts a folder.
func (s *Store) CreateFolder(ctx context.Context, folder *Folder) error {
	if folder.CreatedAt.IsZero() {
		folder.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO folders (id, environment_id, path, created_at)
		VALUES (?,?,?,?)`, folder.ID, folder.EnvironmentID, folder.Path, formatTime(folder.CreatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create folder: %w", err)
	}
	return nil
}

// ListFolders returns an environment's folders.
func (s *Store) ListFolders(ctx context.Context, envID string) ([]Folder, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, environment_id, path, created_at
		FROM folders WHERE environment_id = ? ORDER BY path`, envID)
	if err != nil {
		return nil, fmt.Errorf("store: list folders: %w", err)
	}
	defer rows.Close()
	var out []Folder
	for rows.Next() {
		var folder Folder
		var createdAt string
		if err := rows.Scan(&folder.ID, &folder.EnvironmentID, &folder.Path, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan folder: %w", err)
		}
		if folder.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, folder)
	}
	return out, rows.Err()
}

// GetFolderByPath loads a folder in an environment by its normalized path.
func (s *Store) GetFolderByPath(ctx context.Context, envID, path string) (*Folder, error) {
	var folder Folder
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, environment_id, path, created_at
		FROM folders WHERE environment_id = ? AND path = ?`, envID, path).Scan(
		&folder.ID, &folder.EnvironmentID, &folder.Path, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get folder: %w", err)
	}
	if folder.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &folder, nil
}

// GetFolderByID loads a folder by id.
func (s *Store) GetFolderByID(ctx context.Context, id string) (*Folder, error) {
	var folder Folder
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT id, environment_id, path, created_at
		FROM folders WHERE id = ?`, id).Scan(&folder.ID, &folder.EnvironmentID, &folder.Path, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get folder: %w", err)
	}
	if folder.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &folder, nil
}

// DeleteFolder removes a folder; items keep existing but lose the folder link.
func (s *Store) DeleteFolder(ctx context.Context, envID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM folders WHERE id = ? AND environment_id = ?`, id, envID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("store: delete folder: %w", err)
	}
	return expectOne(res, "delete folder")
}
