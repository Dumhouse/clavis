package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ItemWrite is one operation in an atomic item batch.
type ItemWrite struct {
	Item            *Item
	PreviousVersion int
	ChangeMessage   string
	ActorID         string
}

// ApplyItemBatch creates, updates, and soft-deletes items in one transaction.
// Updates and deletes use PreviousVersion for optimistic concurrency; any
// mismatch rolls the whole batch back with ErrConflict.
func (s *Store) ApplyItemBatch(ctx context.Context, creates, updates, deletes []ItemWrite) error {
	now := time.Now().UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin item batch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, write := range creates {
		item := write.Item
		if item.CreatedAt.IsZero() {
			item.CreatedAt = now
		}
		item.UpdatedAt = now
		if item.Version == 0 {
			item.Version = 1
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO items (
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
			return fmt.Errorf("store: batch create item: %w", err)
		}
		if err := insertItemVersion(ctx, tx, item, write, now); err != nil {
			return err
		}
	}

	for _, write := range updates {
		item := write.Item
		res, err := tx.ExecContext(ctx, `UPDATE items
			SET kind = ?, value_ciphertext = ?, comment_ciphertext = ?, tags = ?, version = ?, updated_at = ?
			WHERE id = ? AND version = ? AND deleted_at IS NULL`,
			item.Kind, item.ValueCiphertext, item.CommentCiphertext, item.Tags, item.Version,
			formatTime(now), item.ID, write.PreviousVersion)
		if err != nil {
			return fmt.Errorf("store: batch update item: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
		item.UpdatedAt = now
		if err := insertItemVersion(ctx, tx, item, write, now); err != nil {
			return err
		}
	}

	for _, write := range deletes {
		item := write.Item
		res, err := tx.ExecContext(ctx, `UPDATE items SET deleted_at = ?, updated_at = ?
			WHERE id = ? AND version = ? AND deleted_at IS NULL`,
			formatTime(now), formatTime(now), item.ID, write.PreviousVersion)
		if err != nil {
			return fmt.Errorf("store: batch delete item: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrConflict
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit item batch: %w", err)
	}
	return nil
}

func insertItemVersion(ctx context.Context, tx *sql.Tx, item *Item, write ItemWrite, now time.Time) error {
	actorID := write.ActorID
	_, err := tx.ExecContext(ctx, `INSERT INTO item_versions
		(item_id, version, value_ciphertext, comment_ciphertext, change_message, actor_id, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		item.ID, item.Version, item.ValueCiphertext, item.CommentCiphertext,
		write.ChangeMessage, actorID, formatTime(now))
	if err != nil {
		return fmt.Errorf("store: batch item version: %w", err)
	}
	return nil
}
