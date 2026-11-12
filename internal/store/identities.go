package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Identity is a machine or agent identity.
type Identity struct {
	ID              string
	OrgID           string
	Name            string
	Type            string
	AuthMethods     string
	TokenTTLSeconds int
	AllowedCommands string
	OIDCTrust       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// AccessToken authenticates an identity. Only the hash is stored.
type AccessToken struct {
	ID         string
	OrgID      string
	IdentityID string
	UserID     string
	TokenHash  []byte
	Prefix     string
	Scopes     string
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	LastUsedIP string
	CreatedAt  time.Time
}

// CreateIdentity inserts an identity.
func (s *Store) CreateIdentity(ctx context.Context, identity *Identity) error {
	now := time.Now().UTC()
	if identity.CreatedAt.IsZero() {
		identity.CreatedAt = now
	}
	if identity.UpdatedAt.IsZero() {
		identity.UpdatedAt = now
	}
	if identity.AuthMethods == "" {
		identity.AuthMethods = "[]"
	}
	if identity.AllowedCommands == "" {
		identity.AllowedCommands = "[]"
	}
	if identity.OIDCTrust == "" {
		identity.OIDCTrust = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO identities
		(id, org_id, name, type, auth_methods, token_ttl_seconds, allowed_commands, oidc_trust, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		identity.ID, identity.OrgID, identity.Name, identity.Type, identity.AuthMethods,
		identity.TokenTTLSeconds, identity.AllowedCommands, identity.OIDCTrust,
		formatTime(identity.CreatedAt), formatTime(identity.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create identity: %w", err)
	}
	return nil
}

const identityColumns = `id, org_id, name, type, auth_methods, token_ttl_seconds,
	allowed_commands, oidc_trust, created_at, updated_at`

func scanIdentity(row rowScanner) (*Identity, error) {
	var (
		identity             Identity
		createdAt, updatedAt string
	)
	err := row.Scan(&identity.ID, &identity.OrgID, &identity.Name, &identity.Type,
		&identity.AuthMethods, &identity.TokenTTLSeconds, &identity.AllowedCommands,
		&identity.OIDCTrust, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan identity: %w", err)
	}
	if identity.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if identity.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &identity, nil
}

// GetIdentity loads an identity.
func (s *Store) GetIdentity(ctx context.Context, orgID, id string) (*Identity, error) {
	return scanIdentity(s.db.QueryRowContext(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE id = ? AND org_id = ?`, id, orgID))
}

// GetIdentityByName loads an identity by name.
func (s *Store) GetIdentityByName(ctx context.Context, orgID, name string) (*Identity, error) {
	return scanIdentity(s.db.QueryRowContext(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE org_id = ? AND lower(name) = lower(?)`, orgID, name))
}

// ListIdentities returns an org's identities.
func (s *Store) ListIdentities(ctx context.Context, orgID string) ([]Identity, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE org_id = ? ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list identities: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Identity
	for rows.Next() {
		identity, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *identity)
	}
	return out, rows.Err()
}

// DeleteIdentity removes an identity and its tokens.
func (s *Store) DeleteIdentity(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM identities WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete identity: %w", err)
	}
	return expectOne(res, "delete identity")
}

// CreateAccessToken inserts a token.
func (s *Store) CreateAccessToken(ctx context.Context, token *AccessToken) error {
	now := time.Now().UTC()
	if token.CreatedAt.IsZero() {
		token.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO access_tokens
		(id, org_id, identity_id, user_id, token_hash, prefix, scopes, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		token.ID, token.OrgID, nullString(token.IdentityID), nullString(token.UserID),
		token.TokenHash, token.Prefix, token.Scopes, nullableTime(token.ExpiresAt), formatTime(token.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create access token: %w", err)
	}
	return nil
}

//nolint:gosec // G101: SQL column list, not a credential
const tokenColumns = `id, org_id, COALESCE(identity_id, ''), COALESCE(user_id, ''), token_hash,
	prefix, scopes, expires_at, last_used_at, COALESCE(last_used_ip, ''), created_at`

func scanAccessToken(row rowScanner) (*AccessToken, error) {
	var (
		token               AccessToken
		expiresAt, lastUsed sql.NullString
		createdAt           string
	)
	err := row.Scan(&token.ID, &token.OrgID, &token.IdentityID, &token.UserID, &token.TokenHash,
		&token.Prefix, &token.Scopes, &expiresAt, &lastUsed, &token.LastUsedIP, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan access token: %w", err)
	}
	if token.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
		return nil, err
	}
	if token.LastUsedAt, err = parseNullableTime(lastUsed); err != nil {
		return nil, err
	}
	if token.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &token, nil
}

// GetAccessTokenByHash loads a token and its identity.
func (s *Store) GetAccessTokenByHash(ctx context.Context, hash []byte) (*AccessToken, *Identity, error) {
	token, err := scanAccessToken(s.db.QueryRowContext(ctx,
		`SELECT `+tokenColumns+` FROM access_tokens WHERE token_hash = ?`, hash))
	if err != nil {
		return nil, nil, err
	}
	if token.IdentityID == "" {
		return token, nil, nil
	}
	identity, err := scanIdentity(s.db.QueryRowContext(ctx,
		`SELECT `+identityColumns+` FROM identities WHERE id = ?`, token.IdentityID))
	if err != nil {
		return nil, nil, err
	}
	return token, identity, nil
}

// ListTokensForIdentity returns metadata for an identity's tokens.
func (s *Store) ListTokensForIdentity(ctx context.Context, identityID string) ([]AccessToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+tokenColumns+` FROM access_tokens
		WHERE identity_id = ? ORDER BY created_at DESC`, identityID)
	if err != nil {
		return nil, fmt.Errorf("store: list tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []AccessToken
	for rows.Next() {
		token, err := scanAccessToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *token)
	}
	return out, rows.Err()
}

// DeleteTokensForIdentity revokes all tokens for an identity.
func (s *Store) DeleteTokensForIdentity(ctx context.Context, identityID string) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM access_tokens WHERE identity_id = ?`, identityID)
	if err != nil {
		return 0, fmt.Errorf("store: delete tokens: %w", err)
	}
	return res.RowsAffected()
}

// TouchAccessToken records token use.
func (s *Store) TouchAccessToken(ctx context.Context, id, ip string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE access_tokens SET last_used_at = ?, last_used_ip = ? WHERE id = ?`,
		formatTime(time.Now()), ip, id)
	if err != nil {
		return fmt.Errorf("store: touch access token: %w", err)
	}
	return nil
}
