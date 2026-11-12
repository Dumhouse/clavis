package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// EmailVerification is a single-use email verification token.
type EmailVerification struct {
	ID        string
	UserID    string
	TokenHash []byte
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
}

// CreateEmailVerification stores a verification token and revokes older ones.
func (s *Store) CreateEmailVerification(ctx context.Context, verification *EmailVerification) error {
	if verification.CreatedAt.IsZero() {
		verification.CreatedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin verification token: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM email_verification_tokens WHERE user_id = ? AND used_at IS NULL`,
		verification.UserID); err != nil {
		return fmt.Errorf("store: revoke verification tokens: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO email_verification_tokens
		(id, user_id, token_hash, created_at, expires_at) VALUES (?,?,?,?,?)`,
		verification.ID, verification.UserID, verification.TokenHash,
		formatTime(verification.CreatedAt), formatTime(verification.ExpiresAt)); err != nil {
		return fmt.Errorf("store: create verification token: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit verification token: %w", err)
	}
	return nil
}

// GetEmailVerificationByHash loads a verification token.
func (s *Store) GetEmailVerificationByHash(ctx context.Context, hash []byte) (*EmailVerification, error) {
	var (
		verification         EmailVerification
		createdAt, expiresAt string
		usedAt               sql.NullString
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id, token_hash, created_at, expires_at, used_at
		FROM email_verification_tokens WHERE token_hash = ?`, hash).Scan(
		&verification.ID, &verification.UserID, &verification.TokenHash, &createdAt, &expiresAt, &usedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get verification token: %w", err)
	}
	if verification.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if verification.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	if verification.UsedAt, err = parseNullableTime(usedAt); err != nil {
		return nil, err
	}
	return &verification, nil
}

// CompleteEmailVerification marks the token used and the user verified.
func (s *Store) CompleteEmailVerification(ctx context.Context, id, userID string) error {
	now := formatTime(time.Now())
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin email verification: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE email_verification_tokens SET used_at = ?
		WHERE id = ? AND used_at IS NULL`, now, id)
	if err != nil {
		return fmt.Errorf("store: mark verification used: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrConflict
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET email_verified = 1, updated_at = ? WHERE id = ?`,
		now, userID); err != nil {
		return fmt.Errorf("store: mark user verified: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit email verification: %w", err)
	}
	return nil
}
