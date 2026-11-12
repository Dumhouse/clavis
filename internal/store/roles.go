package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ListRoles returns an org's built-in and custom roles.
func (s *Store) ListRoles(ctx context.Context, orgID string) ([]Role, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, org_id, name, permissions, builtin
		FROM roles WHERE org_id = ? ORDER BY builtin DESC, name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list roles: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []Role
	for rows.Next() {
		var role Role
		var orgIDValue sql.NullString
		var builtin int
		if err := rows.Scan(&role.ID, &orgIDValue, &role.Name, &role.Permissions, &builtin); err != nil {
			return nil, fmt.Errorf("store: scan role: %w", err)
		}
		role.OrgID = orgIDValue.String
		role.Builtin = builtin != 0
		out = append(out, role)
	}
	return out, rows.Err()
}

// GetRole loads a role in an org.
func (s *Store) GetRole(ctx context.Context, orgID, id string) (*Role, error) {
	var role Role
	var orgIDValue sql.NullString
	var builtin int
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, name, permissions, builtin
		FROM roles WHERE id = ? AND org_id = ?`, id, orgID).Scan(
		&role.ID, &orgIDValue, &role.Name, &role.Permissions, &builtin)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get role: %w", err)
	}
	role.OrgID = orgIDValue.String
	role.Builtin = builtin != 0
	return &role, nil
}

// UpdateRole changes a custom role's name and permissions.
func (s *Store) UpdateRole(ctx context.Context, orgID, id, name, permissions string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE roles SET name = ?, permissions = ?
		WHERE id = ? AND org_id = ? AND builtin = 0`, name, permissions, id, orgID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: update role: %w", err)
	}
	return expectOne(res, "update role")
}

// DeleteRole removes a custom role.
func (s *Store) DeleteRole(ctx context.Context, orgID, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM roles WHERE id = ? AND org_id = ? AND builtin = 0`, id, orgID)
	if err != nil {
		return fmt.Errorf("store: delete role: %w", err)
	}
	return expectOne(res, "delete role")
}

// CountMembersWithRoleID counts active memberships assigned to a role ID.
func (s *Store) CountMembersWithRoleID(ctx context.Context, orgID, roleID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM memberships
		WHERE org_id = ? AND role_id = ? AND status = 'active' AND removed_at IS NULL`,
		orgID, roleID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("store: count role members: %w", err)
	}
	return count, nil
}
