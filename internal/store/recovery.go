package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// RecoveryCode is a hashed one-time MFA recovery code.
type RecoveryCode struct {
	ID        string
	UserID    string
	CodeHash  []byte
	UsedAt    *time.Time
	CreatedAt time.Time
}

// ReplaceRecoveryCodes replaces every recovery code for a user atomically.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID string, hashes [][]byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin recovery codes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM recovery_codes WHERE user_id = ?`, userID); err != nil {
		return fmt.Errorf("store: clear recovery codes: %w", err)
	}
	now := formatTime(time.Now())
	for _, hash := range hashes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recovery_codes (id, user_id, code_hash, created_at)
			VALUES (?,?,?,?)`, uuid.NewString(), userID, hash, now); err != nil {
			return fmt.Errorf("store: insert recovery code: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit recovery codes: %w", err)
	}
	return nil
}

// ListRecoveryCodes returns a user's recovery codes.
func (s *Store) ListRecoveryCodes(ctx context.Context, userID string) ([]RecoveryCode, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, code_hash, used_at, created_at
		FROM recovery_codes WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list recovery codes: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RecoveryCode
	for rows.Next() {
		var code RecoveryCode
		var usedAt sql.NullString
		var createdAt string
		if err := rows.Scan(&code.ID, &code.UserID, &code.CodeHash, &usedAt, &createdAt); err != nil {
			return nil, fmt.Errorf("store: scan recovery code: %w", err)
		}
		if code.UsedAt, err = parseNullableTime(usedAt); err != nil {
			return nil, err
		}
		if code.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, code)
	}
	return out, rows.Err()
}

// ConsumeRecoveryCode marks a matching unused code used. It returns false when
// the code does not exist or was already used.
func (s *Store) ConsumeRecoveryCode(ctx context.Context, userID string, hash []byte) (bool, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE recovery_codes SET used_at = ?
		WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`,
		formatTime(time.Now()), userID, hash)
	if err != nil {
		return false, fmt.Errorf("store: consume recovery code: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// CountUnusedRecoveryCodes returns how many recovery codes remain.
func (s *Store) CountUnusedRecoveryCodes(ctx context.Context, userID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM recovery_codes
		WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count recovery codes: %w", err)
	}
	return n, nil
}
