package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// PasswordReset is a single-use password reset token.
type PasswordReset struct {
	ID        string
	UserID    string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// CreatePasswordReset stores a reset token and revokes the user's older unused
// tokens.
func (s *Store) CreatePasswordReset(ctx context.Context, reset *PasswordReset) error {
	if reset.CreatedAt.IsZero() {
		reset.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin reset token: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM password_reset_tokens WHERE user_id = ? AND used_at IS NULL`,
		reset.UserID); err != nil {
		return fmt.Errorf("store: revoke reset tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO password_reset_tokens
		(id, user_id, token_hash, created_at, expires_at) VALUES (?,?,?,?,?)`,
		reset.ID, reset.UserID, reset.TokenHash, formatTime(reset.CreatedAt), formatTime(reset.ExpiresAt)); err != nil {
		return fmt.Errorf("store: create reset token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit reset token: %w", err)
	}
	return nil
}

// GetPasswordResetByHash loads a reset token by hash.
func (s *Store) GetPasswordResetByHash(ctx context.Context, hash []byte) (*PasswordReset, error) {
	var (
		reset                PasswordReset
		createdAt, expiresAt string
		usedAt               sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, created_at, expires_at, used_at
		FROM password_reset_tokens WHERE token_hash = ?`, hash).Scan(
		&reset.ID, &reset.UserID, &reset.TokenHash, &createdAt, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get reset token: %w", err)
	}
	if reset.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if reset.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	if reset.UsedAt, err = parseNullableTime(usedAt); err != nil {
		return nil, err
	}
	return &reset, nil
}

// CompletePasswordReset changes the password, marks the token used, and revokes
// every session in one transaction.
func (s *Store) CompletePasswordReset(ctx context.Context, resetID, userID, passwordHash string) error {
	now := formatTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin password reset: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `UPDATE password_reset_tokens SET used_at = ?
		WHERE id = ? AND used_at IS NULL`, now, resetID)
	if err != nil {
		return fmt.Errorf("store: mark reset used: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		passwordHash, now, userID); err != nil {
		return fmt.Errorf("store: update password: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: revoke sessions after reset: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit password reset: %w", err)
	}
	return nil
}
