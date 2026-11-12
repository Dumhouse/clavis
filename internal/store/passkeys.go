package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Passkey is a stored WebAuthn credential record.
type Passkey struct {
	ID           string
	UserID       string
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	Data         []byte // serialized webauthn.Credential
	Name         string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// CreatePasskey inserts a passkey.
func (s *Store) CreatePasskey(ctx context.Context, passkey *Passkey) error {
	if passkey.ID == "" {
		passkey.ID = uuid.NewString()
	}
	if passkey.CreatedAt.IsZero() {
		passkey.CreatedAt = time.Now().UTC()
	}
	if passkey.Name == "" {
		passkey.Name = "Passkey"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO passkeys
		(id, user_id, credential_id, public_key, sign_count, data, name, created_at, last_used_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		passkey.ID, passkey.UserID, passkey.CredentialID, passkey.PublicKey, passkey.SignCount,
		passkey.Data, passkey.Name, formatTime(passkey.CreatedAt), nullableTime(passkey.LastUsedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create passkey: %w", err)
	}
	return nil
}

// ListPasskeys returns a user's passkeys.
func (s *Store) ListPasskeys(ctx context.Context, userID string) ([]Passkey, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, credential_id, public_key, sign_count, data, name, created_at, last_used_at
		FROM passkeys WHERE user_id = ? ORDER BY created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list passkeys: %w", err)
	}
	defer rows.Close()
	var out []Passkey
	for rows.Next() {
		passkey, err := scanPasskey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *passkey)
	}
	return out, rows.Err()
}

// DeletePasskey removes one of the user's passkeys.
func (s *Store) DeletePasskey(ctx context.Context, userID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM passkeys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return fmt.Errorf("store: delete passkey: %w", err)
	}
	return expectOne(res, "delete passkey")
}

// UpdatePasskey stores a refreshed credential record and last-used time.
func (s *Store) UpdatePasskey(ctx context.Context, id string, data, publicKey []byte, signCount uint32, lastUsed time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE passkeys
		SET data = ?, public_key = ?, sign_count = ?, last_used_at = ? WHERE id = ?`,
		data, publicKey, signCount, formatTime(lastUsed), id)
	if err != nil {
		return fmt.Errorf("store: update passkey: %w", err)
	}
	return expectOne(res, "update passkey")
}

// GetPasskeyByCredentialID resolves a credential to its owner.
func (s *Store) GetPasskeyByCredentialID(ctx context.Context, credentialID []byte) (*Passkey, error) {
	passkey, err := scanPasskey(s.db.QueryRowContext(ctx, `SELECT id, user_id, credential_id, public_key, sign_count, data, name, created_at, last_used_at
		FROM passkeys WHERE credential_id = ?`, credentialID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return passkey, err
}

func scanPasskey(row rowScanner) (*Passkey, error) {
	var passkey Passkey
	var createdAt string
	var lastUsed sql.NullString
	if err := row.Scan(&passkey.ID, &passkey.UserID, &passkey.CredentialID, &passkey.PublicKey,
		&passkey.SignCount, &passkey.Data, &passkey.Name, &createdAt, &lastUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan passkey: %w", err)
	}
	var err error
	if passkey.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if passkey.LastUsedAt, err = parseNullableTime(lastUsed); err != nil {
		return nil, err
	}
	return &passkey, nil
}
