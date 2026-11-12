package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Project groups environments inside an org.
type Project struct {
	ID                string
	OrgID             string
	Slug              string
	Name              string
	WrappedProjectKey []byte
	KeyVersion        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// Environment is a deployment target inside a project.
type Environment struct {
	ID            string
	ProjectID     string
	Slug          string
	Name          string
	Type          string
	Color         string
	ParentID      *string
	Position      int
	IsProtected   bool
	BranchPattern string
	PreviewSource string
	ExpiresAt     *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CreateProject inserts a project.
func (s *Store) CreateProject(ctx context.Context, p *Project) error {
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects
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
	return nil
}

const projectColumns = `id, org_id, slug, name, wrapped_project_key, key_version, created_at, updated_at`

func scanProject(row rowScanner) (*Project, error) {
	var (
		p                    Project
		createdAt, updatedAt string
	)
	err := row.Scan(&p.ID, &p.OrgID, &p.Slug, &p.Name, &p.WrappedProjectKey, &p.KeyVersion,
		&createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan project: %w", err)
	}
	if p.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if p.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProjectBySlug loads a project by org and slug.
func (s *Store) GetProjectBySlug(ctx context.Context, orgID, slug string) (*Project, error) {
	return scanProject(s.db.QueryRowContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE org_id = ? AND lower(slug) = lower(?)`, orgID, slug))
}

// GetProjectByID loads a project by id.
func (s *Store) GetProjectByID(ctx context.Context, id string) (*Project, error) {
	return scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = ?`, id))
}

// ListProjects returns an org's projects.
func (s *Store) ListProjects(ctx context.Context, orgID string) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+projectColumns+` FROM projects WHERE org_id = ? ORDER BY name`, orgID)
	if err != nil {
		return nil, fmt.Errorf("store: list projects: %w", err)
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// CreateEnvironment inserts an environment.
func (s *Store) CreateEnvironment(ctx context.Context, e *Environment) error {
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO environments
		(id, project_id, slug, name, type, color, parent_id, position, is_protected,
		 branch_pattern, preview_source, expires_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, e.ProjectID, e.Slug, e.Name, e.Type, e.Color, e.ParentID, e.Position, e.IsProtected,
		e.BranchPattern, e.PreviewSource, nullableTime(e.ExpiresAt),
		formatTime(e.CreatedAt), formatTime(e.UpdatedAt))
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: create environment: %w", err)
	}
	return nil
}

const environmentColumns = `id, project_id, slug, name, type, color, parent_id, position,
	is_protected, branch_pattern, preview_source, expires_at, created_at, updated_at`

func scanEnvironment(row rowScanner) (*Environment, error) {
	var (
		e                    Environment
		parentID, expiresAt  sql.NullString
		createdAt, updatedAt string
		protected            int
	)
	err := row.Scan(&e.ID, &e.ProjectID, &e.Slug, &e.Name, &e.Type, &e.Color, &parentID, &e.Position,
		&protected, &e.BranchPattern, &e.PreviewSource, &expiresAt, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: scan environment: %w", err)
	}
	e.IsProtected = protected != 0
	if parentID.Valid {
		e.ParentID = &parentID.String
	}
	if e.ExpiresAt, err = parseNullableTime(expiresAt); err != nil {
		return nil, err
	}
	if e.CreatedAt, err = parseTime(createdAt); err != nil {
		return nil, err
	}
	if e.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return nil, err
	}
	return &e, nil
}

// GetEnvironmentBySlug loads an environment in a project.
func (s *Store) GetEnvironmentBySlug(ctx context.Context, projectID, slug string) (*Environment, error) {
	return scanEnvironment(s.db.QueryRowContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE project_id = ? AND lower(slug) = lower(?)`,
		projectID, slug))
}

// GetEnvironmentByID loads an environment by id.
func (s *Store) GetEnvironmentByID(ctx context.Context, id string) (*Environment, error) {
	return scanEnvironment(s.db.QueryRowContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE id = ?`, id))
}

// ListEnvironments returns a project's environments in display order.
func (s *Store) ListEnvironments(ctx context.Context, projectID string) ([]Environment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+environmentColumns+` FROM environments WHERE project_id = ? ORDER BY position, name`, projectID)
	if err != nil {
		return nil, fmt.Errorf("store: list environments: %w", err)
	}
	defer rows.Close()
	var out []Environment
	for rows.Next() {
		e, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// UpdateEnvironment updates the mutable fields of an environment.
func (s *Store) UpdateEnvironment(ctx context.Context, e *Environment) error {
	res, err := s.db.ExecContext(ctx, `UPDATE environments
		SET slug = ?, name = ?, type = ?, color = ?, parent_id = ?, position = ?,
		    is_protected = ?, branch_pattern = ?, preview_source = ?, expires_at = ?, updated_at = ?
		WHERE id = ?`,
		e.Slug, e.Name, e.Type, e.Color, e.ParentID, e.Position, e.IsProtected,
		e.BranchPattern, e.PreviewSource, nullableTime(e.ExpiresAt), formatTime(time.Now()), e.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: update environment: %w", err)
	}
	return expectOne(res, "update environment")
}

// DeleteEnvironment removes an environment and its items.
func (s *Store) DeleteEnvironment(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete environment: %w", err)
	}
	return expectOne(res, "delete environment")
}

// UpdateProject changes a project's name or slug.
func (s *Store) UpdateProject(ctx context.Context, project *Project) error {
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET slug = ?, name = ?, updated_at = ? WHERE id = ?`,
		project.Slug, project.Name, formatTime(time.Now()), project.ID)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrConflict
		}
		return fmt.Errorf("store: update project: %w", err)
	}
	return expectOne(res, "update project")
}

// DeleteProject removes a project and its environments/items.
func (s *Store) DeleteProject(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete project: %w", err)
	}
	return expectOne(res, "delete project")
}
