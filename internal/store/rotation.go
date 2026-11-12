package store

import (
	"context"
	"fmt"
	"time"
)

// OrgKeyRotation is one org's re-wrapped key material.
type OrgKeyRotation struct {
	OrgID      string
	Wrapped    []byte
	KeyVersion int
}

// UserMFARotation is one user's re-encrypted MFA secret.
type UserMFARotation struct {
	UserID     string
	Ciphertext []byte
	Enabled    bool
}

// ListAllOrgs returns every org. Used only by offline key rotation.
func (s *Store) ListAllOrgs(ctx context.Context) ([]Org, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+orgColumns+` FROM orgs ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: list all orgs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Org
	for rows.Next() {
		var org Org
		var createdAt, updatedAt string
		if err := rows.Scan(&org.ID, &org.Slug, &org.Name, &org.Settings, &org.WrappedOrgKey,
			&org.KeyVersion, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("store: scan org: %w", err)
		}
		var err error
		if org.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if org.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, org)
	}
	return out, rows.Err()
}

// ListUsersWithMFA returns users who have an encrypted TOTP secret.
func (s *Store) ListUsersWithMFA(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users
		WHERE mfa_secret_ciphertext IS NOT NULL AND length(mfa_secret_ciphertext) > 0
		ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("store: list users with MFA: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []User
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *user)
	}
	return out, rows.Err()
}

// ApplyRootKeyRotation commits all re-wrapped org keys and re-encrypted MFA
// secrets atomically.
func (s *Store) ApplyRootKeyRotation(ctx context.Context, orgs []OrgKeyRotation, users []UserMFARotation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin root key rotation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := formatTime(time.Now().UTC())
	for _, org := range orgs {
		if _, err := tx.ExecContext(ctx, `UPDATE orgs SET wrapped_org_key = ?, key_version = ?, updated_at = ?
			WHERE id = ?`, org.Wrapped, org.KeyVersion, now, org.OrgID); err != nil {
			return fmt.Errorf("store: rotate org key %s: %w", org.OrgID, err)
		}
	}
	for _, user := range users {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET mfa_secret_ciphertext = ?, updated_at = ?
			WHERE id = ?`, user.Ciphertext, now, user.UserID); err != nil {
			return fmt.Errorf("store: rotate MFA secret %s: %w", user.UserID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit root key rotation: %w", err)
	}
	return nil
}
