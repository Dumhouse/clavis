package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// SecretSchema describes one project key.
type SecretSchema struct {
	Key         string
	Type        string
	Kind        string
	Required    string // JSON map of environment type -> bool
	Pattern     string
	Description string
	Example     string
	EnumValues  string // JSON array
}

// ListSecretSchema returns a project's schema.
func (s *Store) ListSecretSchema(ctx context.Context, projectID string) ([]SecretSchema, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, type, kind, required, pattern, description, example, enum_values
		FROM secret_schema WHERE project_id = ? ORDER BY key`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list schema: %w", err)
	}
	defer rows.Close()
	var out []SecretSchema
	for rows.Next() {
		var item SecretSchema
		if err := rows.Scan(&item.Key, &item.Type, &item.Kind, &item.Required, &item.Pattern,
			&item.Description, &item.Example, &item.EnumValues); err != nil {
			return nil, fmt.Errorf("store: scan schema: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// ReplaceSecretSchema replaces a project's entire schema atomically.
func (s *Store) ReplaceSecretSchema(ctx context.Context, projectID string, items []SecretSchema) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin schema: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM secret_schema WHERE project_id = ?`, projectID); err != nil {
		return fmt.Errorf("store: clear schema: %w", err)
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO secret_schema
			(project_id, key, type, kind, required, pattern, description, example, enum_values)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			projectID, item.Key, item.Type, item.Kind, item.Required, item.Pattern,
			item.Description, item.Example, item.EnumValues); err != nil {
			return fmt.Errorf("store: insert schema %s: %w", item.Key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit schema: %w", err)
	}
	return nil
}

// DeleteSecretSchema removes one key.
func (s *Store) DeleteSecretSchema(ctx context.Context, projectID, key string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM secret_schema WHERE project_id = ? AND key = ?`, projectID, key)
	if err != nil {
		return fmt.Errorf("store: delete schema: %w", err)
	}
	return expectOne(res, "delete schema")
}

var _ = sql.ErrNoRows
var _ = errors.Is
