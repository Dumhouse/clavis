package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// User is an instance-wide account.
type User struct {
	ID                  string
	Email               string
	Name                string
	AvatarURL           string
	PasswordHash        string
	MFAEnabled          bool
	MFASecretCiphertext []byte
	EmailVerified       bool
	Preferences         string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const userColumns = `id, email, name, avatar_url, COALESCE(password_hash, ''), mfa_enabled, mfa_secret_ciphertext, email_verified, preferences, created_at, updated_at`

func scanUser(row rowScanner) (*User, error) {
	var (
		u                    User
		createdAt, updatedAt string
		mfa, verified        int
	)
	err := row.Scan(&u.ID, &u.Email, &u.Name, &u.AvatarURL, &u.PasswordHash, &mfa,
		&u.MFASecretCiphertext, &verified, &u.Preferences, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan user: %w", err)
	}
	u.MFAEnabled = mfa != 0
	u.EmailVerified = verified != 0
	if u.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if u.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &u, nil
}

// CreateUser inserts a user. The email is stored as given; lookups are
// case-insensitive.
func (s *Store) CreateUser(ctx context.Context, u *User) error {
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	if u.UpdatedAt.IsZero() {
		u.UpdatedAt = now
	}
	preferences := u.Preferences
	if preferences == "" {
		preferences = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users
		(id, email, name, avatar_url, password_hash, mfa_enabled, email_verified, preferences, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.Email, u.Name, u.AvatarURL, nullString(u.PasswordHash), u.MFAEnabled, u.EmailVerified,
		preferences, formatTime(u.CreatedAt), formatTime(u.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create user: %w", err)
	}
	return nil
}

// GetUserByEmail looks up a user case-insensitively.
func (s *Store) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE lower(email) = lower(?)`, strings.TrimSpace(email)))
}

// GetUser loads a user by id.
func (s *Store) GetUser(ctx context.Context, id string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
}

// CountUsers returns the number of accounts. Setup is closed once this is > 0.
func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, fmt.Errorf("store: count users: %w", err)
	}
	return n, nil
}

// SetMFASecret stores an encrypted TOTP secret and the enabled flag.
func (s *Store) SetMFASecret(ctx context.Context, userID string, ciphertext []byte, enabled bool) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET mfa_secret_ciphertext = ?, mfa_enabled = ?, updated_at = ?
		WHERE id = ?`, ciphertext, enabled, formatTime(time.Now()), userID)
	if err != nil {
		return fmt.Errorf("store: set mfa secret: %w", err)
	}
	return expectOne(res, "set mfa secret")
}

// UpdateUserPreferences stores a JSON preferences document.
func (s *Store) UpdateUserPreferences(ctx context.Context, userID, preferences string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET preferences = ?, updated_at = ? WHERE id = ?`,
		preferences, formatTime(time.Now()), userID)
	if err != nil {
		return fmt.Errorf("store: update preferences: %w", err)
	}
	return expectOne(res, "update preferences")
}

// SetPasswordHash updates a user's password hash.
func (s *Store) SetPasswordHash(ctx context.Context, userID, hash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`,
		hash, formatTime(time.Now()), userID)
	if err != nil {
		return fmt.Errorf("store: set password: %w", err)
	}
	return expectOne(res, "set password")
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
