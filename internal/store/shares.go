package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ShareLink is an encrypted one-time link payload. The decryption key is never
// stored on the server.
type ShareLink struct {
	ID           string
	Ciphertext   []byte
	ViewLimit    int
	Views        int
	PasswordHash string
	ExpiresAt    *time.Time
	CreatedAt    time.Time
}

// CreateShareLink stores an encrypted share payload.
func (s *Store) CreateShareLink(ctx context.Context, link *ShareLink) error {
	var expires any
	if link.ExpiresAt != nil {
		expires = formatTime(*link.ExpiresAt)
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO share_links
		(id, ciphertext, view_limit, views, password_hash, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		link.ID, link.Ciphertext, link.ViewLimit, link.Views, link.PasswordHash,
		expires, formatTime(link.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create share link: %w", err)
	}
	return nil
}

// GetShareLink loads a share payload without consuming a view.
func (s *Store) GetShareLink(ctx context.Context, id string) (*ShareLink, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, ciphertext, view_limit, views, password_hash, expires_at, created_at
		FROM share_links WHERE id = ?`, id)
	var link ShareLink
	var passwordHash, expiresAt sql.NullString
	var createdAt string
	if err := row.Scan(&link.ID, &link.Ciphertext, &link.ViewLimit, &link.Views,
		&passwordHash, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	link.PasswordHash = passwordHash.String
	if expiresAt.Valid {
		parsed, err := parseTime(expiresAt.String)
		if err != nil {
			return nil, err
		}
		link.ExpiresAt = &parsed
	}
	parsed, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	link.CreatedAt = parsed
	return &link, nil
}

// ConsumeShareLink atomically increments the view count.
func (s *Store) ConsumeShareLink(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE share_links SET views = views + 1
		WHERE id = ? AND views < view_limit`, id)
	if err != nil {
		return err
	}
	return expectOne(res, "consume share link")
}

// DeleteExpiredShareLinks removes expired payloads.
func (s *Store) DeleteExpiredShareLinks(ctx context.Context, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM share_links
		WHERE expires_at IS NOT NULL AND expires_at <= ?`, formatTime(now))
	if err != nil {
		return fmt.Errorf("store: delete expired share links: %w", err)
	}
	return nil
}
