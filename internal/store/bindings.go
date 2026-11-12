package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// RoleBinding is a scoped grant to a user, group, machine, or agent.
type RoleBinding struct {
	ID              string
	OrgID           string
	SubjectKind     string
	SubjectID       string
	RoleID          string
	RoleName        string
	ProjectID       string
	EnvironmentGlob string
	PathGlob        string
	ExpiresAt       *time.Time
	CreatedAt       time.Time
}

// CreateRoleBinding inserts a scoped grant.
func (s *Store) CreateRoleBinding(ctx context.Context, binding *RoleBinding) error {
	if binding.CreatedAt.IsZero() {
		binding.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO role_bindings
		(id, org_id, subject_kind, subject_id, role_id, project_id, environment_glob, path_glob, expires_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		binding.ID, binding.OrgID, binding.SubjectKind, binding.SubjectID, binding.RoleID,
		nullString(binding.ProjectID), binding.EnvironmentGlob, binding.PathGlob,
		nullableTime(binding.ExpiresAt), formatTime(binding.CreatedAt))
	if err != nil {
		return fmt.Errorf("store: create binding: %w", err)
	}
	return nil
}

const bindingColumns = `rb.id, rb.org_id, rb.subject_kind, rb.subject_id, rb.role_id, COALESCE(r.name, ''),
	COALESCE(rb.project_id, ''), rb.environment_glob, rb.path_glob, rb.expires_at, rb.created_at`

func scanBinding(row rowScanner) (*RoleBinding, error) {
	var (
		binding   RoleBinding
		expiresAt sql.NullString
		createdAt string
	)
	err := row.Scan(&binding.ID, &binding.OrgID, &binding.SubjectKind, &binding.SubjectID, &binding.RoleID,
		&binding.RoleName, &binding.ProjectID, &binding.EnvironmentGlob, &binding.PathGlob,
		&expiresAt, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan binding: %w", err)
	}
	if binding.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
		return nil, err
	}
	if binding.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	return &binding, nil
}

// ListProjectBindings returns grants scoped to a project.
func (s *Store) ListProjectBindings(ctx context.Context, orgID, projectID string) ([]RoleBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+bindingColumns+`
		FROM role_bindings rb LEFT JOIN roles r ON r.id = rb.role_id
		WHERE rb.org_id = ? AND rb.project_id = ?
		ORDER BY rb.created_at`, orgID, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []RoleBinding
	for rows.Next() {
		binding, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *binding)
	}
	return out, rows.Err()
}

// GetRoleBinding loads one grant.
func (s *Store) GetRoleBinding(ctx context.Context, orgID, id string) (*RoleBinding, error) {
	return scanBinding(s.db.QueryRowContext(ctx, `SELECT `+bindingColumns+`
		FROM role_bindings rb LEFT JOIN roles r ON r.id = rb.role_id
		WHERE rb.org_id = ? AND rb.id = ?`, orgID, id))
}

// DeleteRoleBinding revokes a grant.
func (s *Store) DeleteRoleBinding(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM role_bindings WHERE org_id = ? AND id = ?`, orgID, id)
	if err != nil {
		return fmt.Errorf("store: delete binding: %w", err)
	}
	return expectOne(res, "delete binding")
}
