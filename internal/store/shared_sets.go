package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SharedSet is an org-level set of secrets imported by environments.
type SharedSet struct {
	ID         string
	OrgID      string
	Slug       string
	Name       string
	WrappedKey []byte
	KeyVersion int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// SharedSetItem is one encrypted value in a shared set.
type SharedSetItem struct {
	ID                string
	SetID             string
	KeyCiphertext     []byte
	KeyBlindIndex     string
	ValueCiphertext   []byte
	CommentCiphertext []byte
	Kind              string
	Version           int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// SharedSetImport links a set to an environment.
type SharedSetImport struct {
	ID              string
	SetID           string
	EnvironmentID   string
	Priority        int
	CreatedAt       time.Time
	SetSlug         string
	SetName         string
	ProjectSlug     string
	EnvironmentSlug string
}

// CreateSharedSet inserts a set.
func (s *Store) CreateSharedSet(ctx context.Context, set *SharedSet) error {
	now := time.Now().UTC()
	if set.ID == "" {
		set.ID = uuid.NewString()
	}
	if set.CreatedAt.IsZero() {
		set.CreatedAt = now
	}
	set.UpdatedAt = now
	if set.KeyVersion == 0 {
		set.KeyVersion = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO shared_sets
		(id, org_id, slug, name, wrapped_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		set.ID, set.OrgID, set.Slug, set.Name, set.WrappedKey, set.KeyVersion,
		formatTime(set.CreatedAt), formatTime(set.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create shared set: %w", err)
	}
	return nil
}

const sharedSetColumns = `id, org_id, slug, name, wrapped_key, key_version, created_at, updated_at`

func scanSharedSet(row rowScanner) (*SharedSet, error) {
	var set SharedSet
	var createdAt, updatedAt string
	if err := row.Scan(&set.ID, &set.OrgID, &set.Slug, &set.Name, &set.WrappedKey,
		&set.KeyVersion, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan shared set: %w", err)
	}
	var err error
	if set.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if set.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &set, nil
}

// ListSharedSets returns an org's sets.
func (s *Store) ListSharedSets(ctx context.Context, orgID string) ([]SharedSet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sharedSetColumns+` FROM shared_sets
		WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list shared sets: %w", err)
	}
	defer rows.Close()
	var out []SharedSet
	for rows.Next() {
		set, err := scanSharedSet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *set)
	}
	return out, rows.Err()
}

// GetSharedSet resolves a set by slug.
func (s *Store) GetSharedSet(ctx context.Context, orgID, slug string) (*SharedSet, error) {
	return scanSharedSet(s.db.QueryRowContext(ctx, `SELECT `+sharedSetColumns+` FROM shared_sets
		WHERE org_id = ? AND lower(slug) = lower(?)`, orgID, slug))
}

// DeleteSharedSet removes a set and its imports/items.
func (s *Store) DeleteSharedSet(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shared_sets WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete shared set: %w", err)
	}
	return expectOne(res, "delete shared set")
}

const sharedSetItemColumns = `id, set_id, key_ciphertext, key_blind_index, value_ciphertext,
	comment_ciphertext, kind, version, created_at, updated_at`

func scanSharedSetItem(row rowScanner) (*SharedSetItem, error) {
	var item SharedSetItem
	var createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.SetID, &item.KeyCiphertext, &item.KeyBlindIndex,
		&item.ValueCiphertext, &item.CommentCiphertext, &item.Kind, &item.Version,
		&createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan shared set item: %w", err)
	}
	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListSharedSetItems returns a set's items.
func (s *Store) ListSharedSetItems(ctx context.Context, setID string) ([]SharedSetItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sharedSetItemColumns+` FROM shared_set_items
		WHERE set_id = ? ORDER BY created_at`, setID)
	if err != nil {
		return nil, fmt.Errorf("store: list shared set items: %w", err)
	}
	defer rows.Close()
	var out []SharedSetItem
	for rows.Next() {
		item, err := scanSharedSetItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetSharedSetItemByBlindIndex loads one item.
func (s *Store) GetSharedSetItemByBlindIndex(ctx context.Context, setID, blind string) (*SharedSetItem, error) {
	return scanSharedSetItem(s.db.QueryRowContext(ctx, `SELECT `+sharedSetItemColumns+` FROM shared_set_items
		WHERE set_id = ? AND key_blind_index = ?`, setID, blind))
}

// UpsertSharedSetItem creates or replaces an item.
func (s *Store) UpsertSharedSetItem(ctx context.Context, item *SharedSetItem) error {
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	if item.Kind == "" {
		item.Kind = "secret"
	}
	existing, err := s.GetSharedSetItemByBlindIndex(ctx, item.SetID, item.KeyBlindIndex)
	if err == nil {
		item.ID = existing.ID
		item.CreatedAt = existing.CreatedAt
		item.Version = existing.Version + 1
		res, err := s.db.ExecContext(ctx, `UPDATE shared_set_items
			SET key_ciphertext = ?, value_ciphertext = ?, comment_ciphertext = ?, kind = ?,
			    version = ?, updated_at = ? WHERE id = ?`,
			item.KeyCiphertext, item.ValueCiphertext, item.CommentCiphertext, item.Kind,
			item.Version, formatTime(item.UpdatedAt), item.ID)
		if err != nil {
			return fmt.Errorf("store: update shared set item: %w", err)
		}
		return expectOne(res, "update shared set item")
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.Version == 0 {
		item.Version = 1
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO shared_set_items
		(id, set_id, key_ciphertext, key_blind_index, value_ciphertext, comment_ciphertext,
		 kind, version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.SetID, item.KeyCiphertext, item.KeyBlindIndex, item.ValueCiphertext,
		item.CommentCiphertext, item.Kind, item.Version, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	if err != nil {
		return fmt.Errorf("store: create shared set item: %w", err)
	}
	return nil
}

// DeleteSharedSetItem removes an item.
func (s *Store) DeleteSharedSetItem(ctx context.Context, setID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shared_set_items WHERE id = ? AND set_id = ?`, id, setID)
	if err != nil {
		return fmt.Errorf("store: delete shared set item: %w", err)
	}
	return expectOne(res, "delete shared set item")
}

// ImportSharedSet links a set to an environment.
func (s *Store) ImportSharedSet(ctx context.Context, setID, envID string, priority int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO shared_set_imports
		(id, set_id, environment_id, priority, created_at) VALUES (?,?,?,?,?)`,
		uuid.NewString(), setID, envID, priority, formatTime(time.Now().UTC()))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: import shared set: %w", err)
	}
	return nil
}

// DetachSharedSet removes a set from an environment.
func (s *Store) DetachSharedSet(ctx context.Context, setID, envID string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM shared_set_imports
		WHERE set_id = ? AND environment_id = ?`, setID, envID)
	if err != nil {
		return fmt.Errorf("store: detach shared set: %w", err)
	}
	return expectOne(res, "detach shared set")
}

// ListSharedSetImports returns a set's environment imports.
func (s *Store) ListSharedSetImports(ctx context.Context, setID string) ([]SharedSetImport, error) {
	return s.querySharedSetImports(ctx, `WHERE i.set_id = ? ORDER BY i.priority`, setID)
}

// ListEnvironmentSharedSets returns sets imported by an environment in
// ascending priority (later imports override earlier ones).
func (s *Store) ListEnvironmentSharedSets(ctx context.Context, envID string) ([]SharedSetImport, error) {
	return s.querySharedSetImports(ctx, `WHERE i.environment_id = ? ORDER BY i.priority, i.created_at`, envID)
}

func (s *Store) querySharedSetImports(ctx context.Context, where string, arg any) ([]SharedSetImport, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, i.set_id, i.environment_id, i.priority, i.created_at,
			s.slug, s.name, p.slug, e.slug
		FROM shared_set_imports i
		JOIN shared_sets s ON s.id = i.set_id
		JOIN environments e ON e.id = i.environment_id
		JOIN projects p ON p.id = e.project_id `+where, arg)
	if err != nil {
		return nil, fmt.Errorf("store: list shared set imports: %w", err)
	}
	defer rows.Close()
	var out []SharedSetImport
	for rows.Next() {
		var item SharedSetImport
		var createdAt string
		if err := rows.Scan(&item.ID, &item.SetID, &item.EnvironmentID, &item.Priority,
			&createdAt, &item.SetSlug, &item.SetName, &item.ProjectSlug, &item.EnvironmentSlug); err != nil {
			return nil, fmt.Errorf("store: scan shared set import: %w", err)
		}
		if item.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
