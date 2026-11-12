package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Org is an organization (tenant).
type Org struct {
	ID            string
	Slug          string
	Name          string
	Settings      string
	WrappedOrgKey []byte
	KeyVersion    int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// OrgMembership is an org plus the user's role name.
type OrgMembership struct {
	Org             Org
	RoleName        string
	RolePermissions string
}

// Role is a named permission set. Built-in roles have OrgID empty.
type Role struct {
	ID          string
	OrgID       string
	Name        string
	Permissions string
	Builtin     bool
}

// Membership links a user to an org.
type Membership struct {
	OrgID     string
	UserID    string
	RoleID    string
	RoleName  string
	Status    string
	CreatedAt time.Time
	RemovedAt *time.Time
}

// UserBinding is a scoped role binding used by the authorization engine.
type UserBinding struct {
	OrgID           string
	SubjectKind     string
	SubjectID       string
	RoleID          string
	RoleName        string
	RolePermissions string
	ProjectID       string
	EnvironmentGlob string
	PathGlob        string
	ExpiresAt       *time.Time
}

// CreateOrg inserts an org.
func (s *Store) CreateOrg(ctx context.Context, org *Org) error {
	now := time.Now().UTC()
	if org.CreatedAt.IsZero() {
		org.CreatedAt = now
	}
	if org.UpdatedAt.IsZero() {
		org.UpdatedAt = now
	}
	if org.KeyVersion == 0 {
		org.KeyVersion = 1
	}
	if org.Settings == "" {
		org.Settings = "{}"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO orgs
		(id, slug, name, settings, wrapped_org_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		org.ID, org.Slug, org.Name, org.Settings, org.WrappedOrgKey, org.KeyVersion,
		formatTime(org.CreatedAt), formatTime(org.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create org: %w", err)
	}
	return nil
}

const orgColumns = `id, slug, name, settings, wrapped_org_key, key_version, created_at, updated_at`

func scanOrg(row rowScanner) (*Org, error) {
	var (
		org                  Org
		createdAt, updatedAt string
	)
	err := row.Scan(&org.ID, &org.Slug, &org.Name, &org.Settings, &org.WrappedOrgKey,
		&org.KeyVersion, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan org: %w", err)
	}
	if org.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if org.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &org, nil
}

// GetOrgBySlug loads an org by slug, case-insensitively.
func (s *Store) GetOrgBySlug(ctx context.Context, slug string) (*Org, error) {
	return scanOrg(s.db.QueryRowContext(ctx,
		`SELECT `+orgColumns+` FROM orgs WHERE lower(slug) = lower(?)`, slug))
}

// GetOrgByID loads an org by id.
func (s *Store) GetOrgByID(ctx context.Context, id string) (*Org, error) {
	return scanOrg(s.db.QueryRowContext(ctx, `SELECT `+orgColumns+` FROM orgs WHERE id = ?`, id))
}

// ListOrgsForUser returns active org memberships.
func (s *Store) ListOrgsForUser(ctx context.Context, userID string) ([]OrgMembership, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT o.id, o.slug, o.name, o.settings, o.wrapped_org_key, o.key_version,
			o.created_at, o.updated_at, COALESCE(r.name, ''), COALESCE(r.permissions, '[]')
		FROM orgs o
		JOIN memberships m ON m.org_id = o.id
		LEFT JOIN roles r ON r.id = m.role_id
		WHERE m.user_id = ? AND m.status = 'active' AND m.removed_at IS NULL
		ORDER BY o.name`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list orgs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []OrgMembership
	for rows.Next() {
		var (
			org                  Org
			roleName             string
			rolePermissions      string
			createdAt, updatedAt string
		)
		if err := rows.Scan(&org.ID, &org.Slug, &org.Name, &org.Settings, &org.WrappedOrgKey,
			&org.KeyVersion, &createdAt, &updatedAt, &roleName, &rolePermissions); err != nil {
			return nil, fmt.Errorf("store: scan org: %w", err)
		}
		if org.CreatedAt, err = parseTime(createdAt); err != nil {
			return nil, err
		}
		if org.UpdatedAt, err = parseTime(updatedAt); err != nil {
			return nil, err
		}
		out = append(out, OrgMembership{Org: org, RoleName: roleName, RolePermissions: rolePermissions})
	}
	return out, rows.Err()
}

// CreateOrgWithOwner inserts the org, its built-in roles, and the owner's
// membership atomically.
func (s *Store) CreateOrgWithOwner(ctx context.Context, org *Org, roles []*Role, ownerUserID, ownerRoleID string) error {
	now := time.Now().UTC()
	if org.CreatedAt.IsZero() {
		org.CreatedAt = now
	}
	if org.UpdatedAt.IsZero() {
		org.UpdatedAt = now
	}
	if org.KeyVersion == 0 {
		org.KeyVersion = 1
	}
	if org.Settings == "" {
		org.Settings = "{}"
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin create org: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `INSERT INTO orgs
		(id, slug, name, settings, wrapped_org_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		org.ID, org.Slug, org.Name, org.Settings, org.WrappedOrgKey, org.KeyVersion,
		formatTime(org.CreatedAt), formatTime(org.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create org: %w", err)
	}
	for _, role := range roles {
		if _, err := tx.ExecContext(ctx, `INSERT INTO roles (id, org_id, name, permissions, builtin, created_at)
			VALUES (?,?,?,?,?,?)`,
			role.ID, org.ID, role.Name, role.Permissions, role.Builtin, formatTime(now)); err != nil {
			return fmt.Errorf("store: seed role %s: %w", role.Name, err)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)`, org.ID, ownerUserID, ownerRoleID, formatTime(now))
	if err != nil {
		return fmt.Errorf("store: create owner membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit create org: %w", err)
	}
	return nil
}

// CreateProjectWithEnvironments inserts a project and its default environments
// atomically.
func (s *Store) CreateProjectWithEnvironments(ctx context.Context, p *Project, envs []*Environment) error {
	now := time.Now().UTC()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	if p.UpdatedAt.IsZero() {
		p.UpdatedAt = now
	}
	if p.KeyVersion == 0 {
		p.KeyVersion = 1
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin create project: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `INSERT INTO projects
		(id, org_id, slug, name, wrapped_project_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		p.ID, p.OrgID, p.Slug, p.Name, p.WrappedProjectKey, p.KeyVersion,
		formatTime(p.CreatedAt), formatTime(p.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create project: %w", err)
	}
	for _, e := range envs {
		if e.CreatedAt.IsZero() {
			e.CreatedAt = now
		}
		if e.UpdatedAt.IsZero() {
			e.UpdatedAt = now
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO environments
			(id, project_id, slug, name, type, color, parent_id, position, is_protected,
			 branch_pattern, preview_source, expires_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, p.ID, e.Slug, e.Name, e.Type, e.Color, e.ParentID, e.Position, e.IsProtected,
			e.BranchPattern, e.PreviewSource, nullableTime(e.ExpiresAt),
			formatTime(e.CreatedAt), formatTime(e.UpdatedAt))
		if err != nil {
			if isUniqueViolation(err) {
				return ErrConflict
			}
			return fmt.Errorf("store: create environment: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit create project: %w", err)
	}
	return nil
}

// CreateRole inserts a role. permissions is a JSON array.
func (s *Store) CreateRole(ctx context.Context, role *Role) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO roles (id, org_id, name, permissions, builtin, created_at)
		VALUES (?,?,?,?,?,?)`,
		role.ID, nullString(role.OrgID), role.Name, role.Permissions, role.Builtin, formatTime(time.Now()))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create role: %w", err)
	}
	return nil
}

// GetRoleByName loads an org's role by name, case-insensitively.
func (s *Store) GetRoleByName(ctx context.Context, orgID, name string) (*Role, error) {
	var role Role
	var orgIDValue sql.NullString
	var builtin int
	err := s.db.QueryRowContext(ctx, `SELECT id, org_id, name, permissions, builtin FROM roles
		WHERE org_id = ? AND lower(name) = lower(?)`, orgID, name).Scan(
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

// CreateMembership links a user to an org.
func (s *Store) CreateMembership(ctx context.Context, orgID, userID, roleID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)`, orgID, userID, roleID, formatTime(time.Now()))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create membership: %w", err)
	}
	return nil
}

// GetMembership loads one membership with its role name.
func (s *Store) GetMembership(ctx context.Context, orgID, userID string) (*Membership, error) {
	var m Membership
	var roleID sql.NullString
	var roleName sql.NullString
	var createdAt string
	var removedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT m.org_id, m.user_id, m.role_id, r.name, m.status, m.created_at, m.removed_at
		FROM memberships m LEFT JOIN roles r ON r.id = m.role_id
		WHERE m.org_id = ? AND m.user_id = ?`, orgID, userID).Scan(
		&m.OrgID, &m.UserID, &roleID, &roleName, &m.Status, &createdAt, &removedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get membership: %w", err)
	}
	m.RoleID = roleID.String
	m.RoleName = roleName.String
	if m.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if m.RemovedAt, err = parseNullableTime(removedAt); err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteUserBindingsForOrg removes a user's scoped grants in one org.
func (s *Store) DeleteUserBindingsForOrg(ctx context.Context, orgID, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM role_bindings
		WHERE org_id = ? AND subject_kind = 'user' AND subject_id = ?`, orgID, userID)
	if err != nil {
		return fmt.Errorf("store: delete user bindings: %w", err)
	}
	return nil
}

// ListUserBindings returns scoped role bindings for a user.
func (s *Store) ListUserBindings(ctx context.Context, userID string) ([]UserBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rb.org_id, r.name, COALESCE(r.permissions, '[]'),
			COALESCE(rb.project_id, ''), rb.environment_glob, rb.path_glob, rb.expires_at
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		JOIN memberships m ON m.org_id = rb.org_id AND m.user_id = ? AND m.status = 'active' AND m.removed_at IS NULL
		WHERE (rb.subject_kind = 'user' AND rb.subject_id = ?)
		   OR (rb.subject_kind = 'group' AND rb.subject_id IN (
		       SELECT gm.group_id FROM group_members gm WHERE gm.user_id = ?))
		ORDER BY rb.created_at`, userID, userID, userID)
	if err != nil {
		return nil, fmt.Errorf("store: list user bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []UserBinding
	for rows.Next() {
		var b UserBinding
		var expires sql.NullString
		if err := rows.Scan(&b.OrgID, &b.RoleName, &b.RolePermissions, &b.ProjectID, &b.EnvironmentGlob, &b.PathGlob, &expires); err != nil {
			return nil, fmt.Errorf("store: scan user binding: %w", err)
		}
		if b.ExpiresAt, err = parseNullableTime(expires); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ListOrgAccessBindings returns every role binding in an org with its role
// permissions. Used by the access matrix and explainer.
func (s *Store) ListOrgAccessBindings(ctx context.Context, orgID string) ([]UserBinding, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rb.org_id, rb.subject_kind, rb.subject_id, rb.role_id,
			r.name, COALESCE(r.permissions, '[]'), COALESCE(rb.project_id, ''),
			rb.environment_glob, rb.path_glob, rb.expires_at
		FROM role_bindings rb
		JOIN roles r ON r.id = rb.role_id
		WHERE rb.org_id = ?
		ORDER BY rb.created_at`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list org access bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []UserBinding
	for rows.Next() {
		var b UserBinding
		var roleID sql.NullString
		var expires sql.NullString
		if err := rows.Scan(&b.OrgID, &b.SubjectKind, &b.SubjectID, &roleID, &b.RoleName,
			&b.RolePermissions, &b.ProjectID, &b.EnvironmentGlob, &b.PathGlob, &expires); err != nil {
			return nil, fmt.Errorf("store: scan org access binding: %w", err)
		}
		b.RoleID = roleID.String
		if b.ExpiresAt, err = parseNullableTime(expires); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// UpdateOrg changes an org's name or slug.
func (s *Store) UpdateOrg(ctx context.Context, org *Org) error {
	res, err := s.db.ExecContext(ctx, `UPDATE orgs SET slug = ?, name = ?, updated_at = ? WHERE id = ?`,
		org.Slug, org.Name, formatTime(time.Now()), org.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: update org: %w", err)
	}
	return expectOne(res, "update org")
}

// DeleteOrg removes an org and all of its data.
func (s *Store) DeleteOrg(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM orgs WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete org: %w", err)
	}
	return expectOne(res, "delete org")
}

// UpdateOrgSettings replaces an org's JSON settings document.
func (s *Store) UpdateOrgSettings(ctx context.Context, id, settings string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE orgs SET settings = ?, updated_at = ? WHERE id = ?`,
		settings, formatTime(time.Now()), id)
	if err != nil {
		return fmt.Errorf("store: update org settings: %w", err)
	}
	return expectOne(res, "update org settings")
}
