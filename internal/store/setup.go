package store

import (
	"context"
	"fmt"
	"time"
)

// SetupInstance inserts the first user, org, built-in roles, owner membership,
// first project, and default environments in one transaction. This keeps
// first-run setup all-or-nothing so a failure cannot lock the instance.
func (s *Store) SetupInstance(ctx context.Context, user *User, org *Org, roles []*Role, ownerRoleID string, project *Project, envs []*Environment) error {
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
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
	if project.CreatedAt.IsZero() {
		project.CreatedAt = now
	}
	if project.UpdatedAt.IsZero() {
		project.UpdatedAt = now
	}
	if project.KeyVersion == 0 {
		project.KeyVersion = 1
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin setup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	preferences := user.Preferences
	if preferences == "" {
		preferences = "{}"
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO users
		(id, email, name, avatar_url, password_hash, mfa_enabled, email_verified, preferences, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		user.ID, user.Email, user.Name, user.AvatarURL, nullString(user.PasswordHash), user.MFAEnabled,
		user.EmailVerified, preferences, formatTime(user.CreatedAt), formatTime(user.UpdatedAt)); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: setup user: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO orgs
		(id, slug, name, settings, wrapped_org_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		org.ID, org.Slug, org.Name, org.Settings, org.WrappedOrgKey, org.KeyVersion,
		formatTime(org.CreatedAt), formatTime(org.UpdatedAt)); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: setup org: %w", err)
	}

	for _, role := range roles {
		if _, err := tx.ExecContext(ctx, `INSERT INTO roles (id, org_id, name, permissions, builtin, created_at)
			VALUES (?,?,?,?,?,?)`,
			role.ID, org.ID, role.Name, role.Permissions, role.Builtin, formatTime(now)); err != nil {
			return fmt.Errorf("store: setup role %s: %w", role.Name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO memberships (org_id, user_id, role_id, status, created_at)
		VALUES (?,?,?, 'active', ?)`, org.ID, user.ID, ownerRoleID, formatTime(now)); err != nil {
		return fmt.Errorf("store: setup membership: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `INSERT INTO projects
		(id, org_id, slug, name, wrapped_project_key, key_version, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?)`,
		project.ID, project.OrgID, project.Slug, project.Name, project.WrappedProjectKey,
		project.KeyVersion, formatTime(project.CreatedAt), formatTime(project.UpdatedAt)); err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: setup project: %w", err)
	}

	for _, e := range envs {
		if e.CreatedAt.IsZero() {
			e.CreatedAt = now
		}
		if e.UpdatedAt.IsZero() {
			e.UpdatedAt = now
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO environments
			(id, project_id, slug, name, type, color, parent_id, position, is_protected,
			 branch_pattern, preview_source, expires_at, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			e.ID, project.ID, e.Slug, e.Name, e.Type, e.Color, e.ParentID, e.Position, e.IsProtected,
			e.BranchPattern, e.PreviewSource, nullableTime(e.ExpiresAt),
			formatTime(e.CreatedAt), formatTime(e.UpdatedAt)); err != nil {
			return fmt.Errorf("store: setup environment: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit setup: %w", err)
	}
	return nil
}
