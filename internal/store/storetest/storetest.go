// Package storetest provides SQLite fixtures and factories for integration
// tests. It is test-only code and must not be imported by production packages.
package storetest

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/store"
)

// Open returns a migrated, empty SQLite store that is closed automatically at
// the end of the test.
func Open(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	s, err := store.Open(ctx, filepath.Join(t.TempDir(), "clavis.db"))
	if err != nil {
		t.Fatalf("storetest: open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("storetest: migrate: %v", err)
	}
	return s
}

// CreateUser inserts a user and returns its id.
func CreateUser(t *testing.T, s *store.Store, email string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	_, err := s.DB().ExecContext(context.Background(), `INSERT INTO users
		(id, email, name, created_at, updated_at) VALUES (?,?,?,?,?)`,
		id, email, email, now.Format("2006-01-02T15:04:05.000000000Z"), now.Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		t.Fatalf("storetest: create user: %v", err)
	}
	return id
}

// CreateOrg inserts an org and returns its id.
func CreateOrg(t *testing.T, s *store.Store, slug string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err := s.DB().ExecContext(context.Background(), `INSERT INTO orgs
		(id, slug, name, wrapped_org_key, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
		id, slug, slug, []byte("test-wrapped-key"), now, now)
	if err != nil {
		t.Fatalf("storetest: create org: %v", err)
	}
	return id
}

// CreateProject inserts a project into an org and returns its id.
func CreateProject(t *testing.T, s *store.Store, orgID, slug string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err := s.DB().ExecContext(context.Background(), `INSERT INTO projects
		(id, org_id, slug, name, wrapped_project_key, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, orgID, slug, slug, []byte("test-wrapped-key"), now, now)
	if err != nil {
		t.Fatalf("storetest: create project: %v", err)
	}
	return id
}

// CreateEnvironment inserts an environment and returns its id.
func CreateEnvironment(t *testing.T, s *store.Store, projectID, slug, envType string) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	_, err := s.DB().ExecContext(context.Background(), `INSERT INTO environments
		(id, project_id, slug, name, type, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, projectID, slug, slug, envType, now, now)
	if err != nil {
		t.Fatalf("storetest: create environment: %v", err)
	}
	return id
}
