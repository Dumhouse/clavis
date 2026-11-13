package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SyncConnection is an org-level provider credential shared by syncs. Config
// holds provider fields as JSON; secret fields are org-key ciphertext bound to
// the connection ID, never plaintext.
type SyncConnection struct {
	ID         string
	OrgID      string
	Provider   string
	Name       string
	BaseURL    string
	Config     string
	CreatedBy  string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	LastUsedAt *time.Time
}

// SyncConnectionUsage is one sync that references a connection.
type SyncConnectionUsage struct {
	SyncID      string
	ProjectID   string
	ProjectSlug string
	Environment string
	Provider    string
}

const connectionColumns = `id, org_id, provider, name, base_url, config, created_by,
	created_at, updated_at, last_used_at`

func scanConnection(row rowScanner) (*SyncConnection, error) {
	var conn SyncConnection
	var createdAt, updatedAt string
	var lastUsed sql.NullString
	if err := row.Scan(&conn.ID, &conn.OrgID, &conn.Provider, &conn.Name, &conn.BaseURL,
		&conn.Config, &conn.CreatedBy, &createdAt, &updatedAt, &lastUsed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("store: scan connection: %w", err)
	}
	var err error
	if conn.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if conn.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	if conn.LastUsedAt, err = parseNullableTime(lastUsed); err != nil {
		return nil, err
	}
	return &conn, nil
}

// CreateSyncConnection inserts a connection. A duplicate name in the org
// returns ErrConflict.
func (s *Store) CreateSyncConnection(ctx context.Context, conn *SyncConnection) error {
	if conn.ID == "" {
		conn.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if conn.CreatedAt.IsZero() {
		conn.CreatedAt = now
	}
	conn.UpdatedAt = conn.CreatedAt
	if conn.Config == "" {
		conn.Config = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_connections
		(id, org_id, provider, name, base_url, config, created_by, created_at, updated_at, last_used_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		conn.ID, conn.OrgID, conn.Provider, conn.Name, conn.BaseURL, conn.Config, conn.CreatedBy,
		formatTime(conn.CreatedAt), formatTime(conn.UpdatedAt), nullableTime(conn.LastUsedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create connection: %w", err)
	}
	return nil
}

// ListSyncConnections returns an org's connections ordered by name.
func (s *Store) ListSyncConnections(ctx context.Context, orgID string) ([]SyncConnection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+connectionColumns+` FROM sync_connections
		WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list connections: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []SyncConnection
	for rows.Next() {
		conn, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *conn)
	}
	return out, rows.Err()
}

// GetSyncConnection loads one connection in an org.
func (s *Store) GetSyncConnection(ctx context.Context, orgID, id string) (*SyncConnection, error) {
	return scanConnection(s.db.QueryRowContext(ctx, `SELECT `+connectionColumns+` FROM sync_connections
		WHERE id = ? AND org_id = ?`, id, orgID))
}

// UpdateSyncConnection replaces the name, base URL, and config.
func (s *Store) UpdateSyncConnection(ctx context.Context, conn *SyncConnection) error {
	conn.UpdatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `UPDATE sync_connections
		SET name = ?, base_url = ?, config = ?, updated_at = ? WHERE id = ? AND org_id = ?`,
		conn.Name, conn.BaseURL, conn.Config, formatTime(conn.UpdatedAt), conn.ID, conn.OrgID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: update connection: %w", err)
	}
	return expectOne(res, "update connection")
}

// TouchSyncConnection records that a sync used the connection.
func (s *Store) TouchSyncConnection(ctx context.Context, id string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sync_connections SET last_used_at = ? WHERE id = ?`,
		formatTime(at), id)
	if err != nil {
		return fmt.Errorf("store: touch connection: %w", err)
	}
	return nil
}

// DeleteSyncConnection removes a connection. Callers check usage first.
func (s *Store) DeleteSyncConnection(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sync_connections WHERE id = ? AND org_id = ?`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete connection: %w", err)
	}
	return expectOne(res, "delete connection")
}

// ListSyncConnectionUsage returns the syncs in an org that use a connection.
// An empty connectionID lists every sync that uses any connection.
func (s *Store) ListSyncConnectionUsage(ctx context.Context, orgID, connectionID string) (map[string][]SyncConnectionUsage, error) {
	query := `SELECT s.connection_id, s.id, s.project_id, p.slug, COALESCE(e.slug, ''), s.provider
		FROM syncs s
		JOIN projects p ON p.id = s.project_id
		LEFT JOIN environments e ON e.id = s.environment_id
		WHERE p.org_id = ? AND s.connection_id <> ''`
	args := []any{orgID}
	if connectionID != "" {
		query += ` AND s.connection_id = ?`
		args = append(args, connectionID)
	}
	rows, err := s.db.QueryContext(ctx, query+` ORDER BY p.slug, s.created_at`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list connection usage: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string][]SyncConnectionUsage{}
	for rows.Next() {
		var connID string
		var usage SyncConnectionUsage
		if err := rows.Scan(&connID, &usage.SyncID, &usage.ProjectID, &usage.ProjectSlug,
			&usage.Environment, &usage.Provider); err != nil {
			return nil, fmt.Errorf("store: scan connection usage: %w", err)
		}
		out[connID] = append(out[connID], usage)
	}
	return out, rows.Err()
}

// SetSyncConnection points a sync at a connection and replaces its config
// (which drops any inline credential).
func (s *Store) SetSyncConnection(ctx context.Context, projectID, id, connectionID, config string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE syncs SET connection_id = ?, config = ?, updated_at = ?
		WHERE id = ? AND project_id = ?`, connectionID, config, formatTime(time.Now().UTC()), id, projectID)
	if err != nil {
		return fmt.Errorf("store: set sync connection: %w", err)
	}
	return expectOne(res, "set sync connection")
}
