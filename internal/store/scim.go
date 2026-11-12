package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ScimToken authenticates a SCIM provisioning client for one organization.
type ScimToken struct {
	ID         string
	OrgID      string
	TokenHash  []byte
	Prefix     string
	CreatedBy  string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateScimToken inserts a SCIM bearer token.
func (s *Store) CreateScimToken(ctx context.Context, token *ScimToken) error {
	if token.ID == "" {
		token.ID = uuid.NewString()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO scim_tokens
		(id, org_id, token_hash, prefix, created_by, created_at)
		VALUES (?,?,?,?,?,?)`,
		token.ID, token.OrgID, token.TokenHash, token.Prefix, token.CreatedBy,
		formatTime(token.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create scim token: %w", err)
	}
	return nil
}

// GetScimTokenByHash resolves a SCIM token.
func (s *Store) GetScimTokenByHash(ctx context.Context, hash []byte) (*ScimToken, error) {
	var token ScimToken
	var createdAt string
	var lastUsed sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, token_hash, prefix, created_by, created_at, last_used_at
		FROM scim_tokens WHERE token_hash = ?`, hash).Scan(
		&token.ID, &token.OrgID, &token.TokenHash, &token.Prefix, &token.CreatedBy,
		&createdAt, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get scim token: %w", err)
	}
	if token.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if token.LastUsedAt, err = parseNullableTime(lastUsed); err != nil {
		return nil, err
	}
	return &token, nil
}

// TouchScimToken records use.
func (s *Store) TouchScimToken(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scim_tokens SET last_used_at = ? WHERE id = ?`,
		formatTime(time.Now().UTC()), id)
	return err
}

// ListScimTokens returns an org's SCIM tokens without hashes.
func (s *Store) ListScimTokens(ctx context.Context, orgID string) ([]ScimToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, prefix, created_by, created_at, last_used_at
		FROM scim_tokens WHERE org_id = ? ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list scim tokens: %w", err)
	}
	defer rows.Close()
	var out []ScimToken
	for rows.Next() {
		var token ScimToken
		var createdAt string
		var lastUsed sql.NullString
		if err := rows.Scan(&token.ID, &token.OrgID, &token.Prefix, &token.CreatedBy, &createdAt, &lastUsed); err != nil {
			return nil, fmt.Errorf("store: scan scim token: %w", err)
		}
		if token.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if token.LastUsedAt, err = parseNullableTime(lastUsed); err != nil {
			return nil, err
		}
		out = append(out, token)
	}
	return out, rows.Err()
}

// DeleteScimToken revokes one token.
func (s *Store) DeleteScimToken(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM scim_tokens WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete scim token: %w", err)
	}
	return expectOne(res, "delete scim token")
}

// ActivateMembership re-adds or updates a membership.
func (s *Store) ActivateMembership(ctx context.Context, orgID, userID, roleID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)
		ON CONFLICT(org_id, user_id) DO UPDATE SET
			role_id = excluded.role_id, status = 'active', removed_at = NULL`,
		orgID, userID, roleID, formatTime(time.Now().UTC()))
	if err != nil {
		return fmt.Errorf("store: activate membership: %w", err)
	}
	return nil
}

// UpdateUserName changes a user's display name.
func (s *Store) UpdateUserName(ctx context.Context, userID, name string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET name = ?, updated_at = ? WHERE id = ?`,
		name, formatTime(time.Now().UTC()), userID)
	if err != nil {
		return fmt.Errorf("store: update user name: %w", err)
	}
	return expectOne(res, "update user name")
}
