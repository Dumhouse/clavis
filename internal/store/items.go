package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Item is an encrypted secret, variable, or file.
type Item struct {
	ID                string
	EnvironmentID     string
	OverlayID         *string
	FolderID          *string
	Kind              string
	KeyCiphertext     []byte
	KeyBlindIndex     string
	ValueCiphertext   []byte
	CommentCiphertext []byte
	Tags              string
	OwnerGroupID      *string
	NotesCiphertext   []byte
	ProviderURL       string
	ExpiresAt         *time.Time
	Version           int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	DeletedAt         *time.Time
}

// ItemVersion is one immutable revision of an item.
type ItemVersion struct {
	ItemID            string
	Version           int
	ValueCiphertext   []byte
	CommentCiphertext []byte
	ChangeMessage     string
	ActorID           string
	CreatedAt         time.Time
}

// CreateItem inserts an item and its first version atomically.
func (s *Store) CreateItem(ctx context.Context, item *Item, changeMessage string) error {
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if item.Version == 0 {
		item.Version = 1
	}
	if item.Tags == "" {
		item.Tags = "[]"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin create item: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `INSERT INTO items (
		id, environment_id, overlay_id, folder_id, kind, key_ciphertext, key_blind_index,
		value_ciphertext, comment_ciphertext, tags, owner_group_id, notes_ciphertext,
		provider_url, expires_at, version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.ID, item.EnvironmentID, item.OverlayID, item.FolderID, item.Kind,
		item.KeyCiphertext, item.KeyBlindIndex, item.ValueCiphertext, item.CommentCiphertext,
		item.Tags, item.OwnerGroupID, item.NotesCiphertext, item.ProviderURL,
		nullableTime(item.ExpiresAt), item.Version, formatTime(item.CreatedAt), formatTime(item.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create item: %w", err)
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO item_versions
		(item_id, version, value_ciphertext, comment_ciphertext, change_message, actor_id, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		item.ID, item.Version, item.ValueCiphertext, item.CommentCiphertext,
		changeMessage, "", formatTime(item.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create item version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit create item: %w", err)
	}
	return nil
}

const itemColumns = `id, environment_id, overlay_id, folder_id, kind, key_ciphertext, key_blind_index,
	value_ciphertext, comment_ciphertext, tags, owner_group_id, notes_ciphertext,
	provider_url, expires_at, version, created_at, updated_at, deleted_at`

func scanItem(row rowScanner) (*Item, error) {
	var (
		item                              Item
		overlayID, folderID, ownerGroupID sql.NullString
		expiresAt, deletedAt              sql.NullString
		createdAt, updatedAt              string
	)
	err := row.Scan(&item.ID, &item.EnvironmentID, &overlayID, &folderID, &item.Kind,
		&item.KeyCiphertext, &item.KeyBlindIndex, &item.ValueCiphertext, &item.CommentCiphertext,
		&item.Tags, &ownerGroupID, &item.NotesCiphertext, &item.ProviderURL, &expiresAt,
		&item.Version, &createdAt, &updatedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan item: %w", err)
	}
	if overlayID.Valid {
		item.OverlayID = &overlayID.String
	}
	if folderID.Valid {
		item.FolderID = &folderID.String
	}
	if ownerGroupID.Valid {
		item.OwnerGroupID = &ownerGroupID.String
	}
	if item.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
		return nil, err
	}
	if item.DeletedAt, err = parseNullableTime(deletedAt); err != nil {
		return nil, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetItemByBlindIndex finds a live item in an environment by its key's blind
// index. Overlay items are handled in M2.
func (s *Store) GetItemByBlindIndex(ctx context.Context, envID, blindIndex string) (*Item, error) {
	return scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items
		WHERE environment_id = ? AND overlay_id IS NULL AND deleted_at IS NULL AND key_blind_index = ?`,
		envID, blindIndex))
}

// GetItemByID loads a live item by id.
func (s *Store) GetItemByID(ctx context.Context, id string) (*Item, error) {
	return scanItem(s.db.QueryRowContext(ctx, `SELECT `+itemColumns+` FROM items
		WHERE id = ? AND deleted_at IS NULL`, id))
}

// ListItems returns live items in an environment.
func (s *Store) ListItems(ctx context.Context, envID string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+itemColumns+` FROM items
		WHERE environment_id = ? AND overlay_id IS NULL AND deleted_at IS NULL
		ORDER BY updated_at DESC`, envID)
	if err != nil {
		return nil, fmt.Errorf("store: list items: %w", err)
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

// UpdateItem writes a new value and appends an immutable version atomically.
func (s *Store) UpdateItem(ctx context.Context, item *Item, changeMessage, actorID string) error {
	now := time.Now().UTC()
	item.Version++
	item.UpdatedAt = now

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin update item: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `UPDATE items
		SET kind = ?, value_ciphertext = ?, comment_ciphertext = ?, tags = ?, folder_id = ?,
		    notes_ciphertext = ?, provider_url = ?, version = ?, updated_at = ?
		WHERE id = ? AND deleted_at IS NULL`,
		item.Kind, item.ValueCiphertext, item.CommentCiphertext, item.Tags, item.FolderID,
		item.NotesCiphertext, item.ProviderURL, item.Version, formatTime(now), item.ID)
	if err != nil {
		return fmt.Errorf("store: update item: %w", err)
	}
	if err := expectOne(res, "update item"); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO item_versions
		(item_id, version, value_ciphertext, comment_ciphertext, change_message, actor_id, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		item.ID, item.Version, item.ValueCiphertext, item.CommentCiphertext,
		changeMessage, actorID, formatTime(now))
	if err != nil {
		return fmt.Errorf("store: insert item version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit update item: %w", err)
	}
	return nil
}

// SoftDeleteItem marks an item deleted without destroying history.
func (s *Store) SoftDeleteItem(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE items SET deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`,
		formatTime(time.Now()), formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("store: delete item: %w", err)
	}
	return expectOne(res, "delete item")
}

// ListItemVersions returns revisions newest first. Values are included; the
// service only decrypts them when the caller may read values.
func (s *Store) ListItemVersions(ctx context.Context, itemID string) ([]ItemVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT item_id, version, value_ciphertext, comment_ciphertext,
			change_message, actor_id, created_at
		FROM item_versions WHERE item_id = ? ORDER BY version DESC`, itemID)
	if err != nil {
		return nil, fmt.Errorf("store: list item versions: %w", err)
	}
	defer rows.Close()
	var out []ItemVersion
	for rows.Next() {
		var v ItemVersion
		var createdAt string
		if err := rows.Scan(&v.ItemID, &v.Version, &v.ValueCiphertext, &v.CommentCiphertext,
			&v.ChangeMessage, &v.ActorID, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan item version: %w", err)
		}
		if v.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetItemVersion loads one revision.
func (s *Store) GetItemVersion(ctx context.Context, itemID string, version int) (*ItemVersion, error) {
	var v ItemVersion
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT item_id, version, value_ciphertext, comment_ciphertext,
			change_message, actor_id, created_at
		FROM item_versions WHERE item_id = ? AND version = ?`, itemID, version).Scan(
		&v.ItemID, &v.Version, &v.ValueCiphertext, &v.CommentCiphertext,
		&v.ChangeMessage, &v.ActorID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get item version: %w", err)
	}
	if v.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &v, nil
}

// InsertRevealEvent records that a human saw an item value.
func (s *Store) InsertRevealEvent(ctx context.Context, id, userID, itemID, channel string, version int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO reveal_events (id, user_id, item_id, version, channel, created_at)
		VALUES (?,?,?,?,?,?)`, id, userID, itemID, version, channel, formatTime(time.Now()))
	if err != nil {
		return fmt.Errorf("store: insert reveal event: %w", err)
	}
	return nil
}
