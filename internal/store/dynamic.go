package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// DynamicSecretConfig configures a dynamic secret engine.
type DynamicSecretConfig struct {
	ID         string
	ProjectID  string
	Engine     string
	Config     []byte
	DefaultTTL int
	MaxTTL     int
	CreatedAt  time.Time
}

// Lease is a dynamic credential lease.
type Lease struct {
	ID         string
	ConfigID   string
	IdentityID string
	LeaseData  []byte
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	CreatedAt  time.Time
}

func (s *Store) CreateDynamicSecretConfig(ctx context.Context, config *DynamicSecretConfig) error {
	now := time.Now().UTC()
	if config.CreatedAt.IsZero() {
		config.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO dynamic_secret_configs
		(id, project_id, engine, config, default_ttl_seconds, max_ttl_seconds, created_at)
		VALUES (?,?,?,?,?,?,?)`,
		config.ID, config.ProjectID, config.Engine, config.Config,
		config.DefaultTTL, config.MaxTTL, formatTime(config.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create dynamic config: %w", err)
	}
	return nil
}

func (s *Store) ListDynamicSecretConfigs(ctx context.Context, projectID string) ([]DynamicSecretConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, project_id, engine, config, default_ttl_seconds, max_ttl_seconds, created_at
		FROM dynamic_secret_configs WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list dynamic configs: %w", err)
	}
	defer rows.Close()
	var out []DynamicSecretConfig
	for rows.Next() {
		var config DynamicSecretConfig
		var createdAt string
		if err := rows.Scan(&config.ID, &config.ProjectID, &config.Engine, &config.Config,
			&config.DefaultTTL, &config.MaxTTL, &createdAt); err != nil {
			return nil, err
		}
		if config.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, config)
	}
	return out, rows.Err()
}

func (s *Store) GetDynamicSecretConfigByID(ctx context.Context, id string) (*DynamicSecretConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, engine, config, default_ttl_seconds, max_ttl_seconds, created_at
		FROM dynamic_secret_configs WHERE id = ?`, id)
	return scanDynamicConfig(row)
}

func (s *Store) GetDynamicSecretConfig(ctx context.Context, projectID, id string) (*DynamicSecretConfig, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, engine, config, default_ttl_seconds, max_ttl_seconds, created_at
		FROM dynamic_secret_configs WHERE project_id = ? AND id = ?`, projectID, id)
	return scanDynamicConfig(row)
}

func scanDynamicConfig(row rowScanner) (*DynamicSecretConfig, error) {
	var config DynamicSecretConfig
	var createdAt string
	err := row.Scan(&config.ID, &config.ProjectID, &config.Engine, &config.Config,
		&config.DefaultTTL, &config.MaxTTL, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get dynamic config: %w", err)
	}
	if config.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &config, nil
}

func (s *Store) DeleteDynamicSecretConfig(ctx context.Context, projectID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM dynamic_secret_configs WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	return expectOne(res, "delete dynamic config")
}

func (s *Store) CreateLease(ctx context.Context, lease *Lease) error {
	now := time.Now().UTC()
	if lease.CreatedAt.IsZero() {
		lease.CreatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO leases
		(id, config_id, lease_data, expires_at, created_at, identity_id)
		VALUES (?,?,?,?,?,?)`,
		lease.ID, lease.ConfigID, lease.LeaseData, formatTime(lease.ExpiresAt),
		formatTime(lease.CreatedAt), lease.IdentityID)
	if err != nil {
		return fmt.Errorf("store: create lease: %w", err)
	}
	return nil
}

func (s *Store) GetLease(ctx context.Context, id string) (*Lease, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, config_id, lease_data, expires_at, revoked_at, created_at, identity_id
		FROM leases WHERE id = ?`, id)
	var lease Lease
	var expiresAt, createdAt string
	var revokedAt sql.NullString
	if err := row.Scan(&lease.ID, &lease.ConfigID, &lease.LeaseData, &expiresAt, &revokedAt,
		&createdAt, &lease.IdentityID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: get lease: %w", err)
	}
	var err error
	if lease.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return nil, err
	}
	if lease.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if revokedAt.Valid {
		parsed, err := parseTime(revokedAt.String)
		if err != nil {
			return nil, err
		}
		lease.RevokedAt = &parsed
	}
	return &lease, nil
}

func (s *Store) ListLeases(ctx context.Context, configID string, includeRevoked bool) ([]Lease, error) {
	query := `SELECT id, config_id, lease_data, expires_at, revoked_at, created_at, identity_id
		FROM leases WHERE config_id = ?`
	if !includeRevoked {
		query += ` AND revoked_at IS NULL`
	}
	query += ` ORDER BY created_at DESC LIMIT 500`
	rows, err := s.db.QueryContext(ctx, query, configID)
	if err != nil {
		return nil, fmt.Errorf("store: list leases: %w", err)
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		var lease Lease
		var expiresAt, createdAt string
		var revokedAt sql.NullString
		if err := rows.Scan(&lease.ID, &lease.ConfigID, &lease.LeaseData, &expiresAt, &revokedAt,
			&createdAt, &lease.IdentityID); err != nil {
			return nil, err
		}
		var err error
		if lease.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return nil, err
		}
		if lease.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if revokedAt.Valid {
			parsed, err := parseTime(revokedAt.String)
			if err != nil {
				return nil, err
			}
			lease.RevokedAt = &parsed
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func (s *Store) ListExpiredLeases(ctx context.Context, now time.Time) ([]Lease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, config_id, lease_data, expires_at, revoked_at, created_at, identity_id
		FROM leases WHERE revoked_at IS NULL AND expires_at <= ?`, formatTime(now))
	if err != nil {
		return nil, fmt.Errorf("store: list expired leases: %w", err)
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		var lease Lease
		var expiresAt, createdAt string
		var revokedAt sql.NullString
		if err := rows.Scan(&lease.ID, &lease.ConfigID, &lease.LeaseData, &expiresAt, &revokedAt,
			&createdAt, &lease.IdentityID); err != nil {
			return nil, err
		}
		if lease.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return nil, err
		}
		if lease.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func (s *Store) RevokeLease(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE leases SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		formatTime(at), id)
	if err != nil {
		return err
	}
	return expectOne(res, "revoke lease")
}

func (s *Store) ListLeasesByIdentity(ctx context.Context, identityID string) ([]Lease, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, config_id, lease_data, expires_at, revoked_at, created_at, identity_id
		FROM leases WHERE identity_id = ? AND revoked_at IS NULL`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Lease
	for rows.Next() {
		var lease Lease
		var expiresAt, createdAt string
		var revokedAt sql.NullString
		if err := rows.Scan(&lease.ID, &lease.ConfigID, &lease.LeaseData, &expiresAt,
			&revokedAt, &createdAt, &lease.IdentityID); err != nil {
			return nil, err
		}
		var err error
		if lease.ExpiresAt, err = parseTime(expiresAt); err != nil {
			return nil, err
		}
		if lease.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if revokedAt.Valid {
			parsed, err := parseTime(revokedAt.String)
			if err != nil {
				return nil, err
			}
			lease.RevokedAt = &parsed
		}
		out = append(out, lease)
	}
	return out, rows.Err()
}

func (s *Store) UpdateLeaseExpiry(ctx context.Context, id string, expiresAt time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE leases SET expires_at = ? WHERE id = ? AND revoked_at IS NULL`,
		formatTime(expiresAt), id)
	if err != nil {
		return err
	}
	return expectOne(res, "renew lease")
}
