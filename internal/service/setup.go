package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// SetupInput describes the first org, project, and environments.
type SetupInput struct {
	OrgName     string
	OrgSlug     string
	ProjectName string
	ProjectSlug string
}

// Setup creates the first user, org, project, and default environments
// atomically. The caller must have verified the one-time setup token.
func (s *Service) Setup(ctx context.Context, user *store.User, in SetupInput) (*store.Org, *store.Project, []store.Environment, error) {
	orgName := strings.TrimSpace(in.OrgName)
	if orgName == "" || len(orgName) > 100 {
		return nil, nil, nil, fmt.Errorf("%w: organization name is required", ErrValidation)
	}
	if strings.TrimSpace(in.OrgSlug) == "" {
		in.OrgSlug = Slugify(orgName)
	}
	projectName := strings.TrimSpace(in.ProjectName)
	if projectName == "" {
		projectName = "Default"
	}
	if len(projectName) > 100 {
		return nil, nil, nil, fmt.Errorf("%w: project name must be under 100 characters", ErrValidation)
	}
	if strings.TrimSpace(in.ProjectSlug) == "" {
		in.ProjectSlug = Slugify(projectName)
	}
	orgSlug, err := NormalizeSlug(in.OrgSlug)
	if err != nil {
		return nil, nil, nil, err
	}
	projectSlug, err := NormalizeSlug(in.ProjectSlug)
	if err != nil {
		return nil, nil, nil, err
	}

	orgID := uuid.NewString()
	wrappedOrg, _, err := s.Keyring.NewOrgKey(orgID)
	if err != nil {
		return nil, nil, nil, err
	}
	org := &store.Org{ID: orgID, Slug: orgSlug, Name: orgName, WrappedOrgKey: wrappedOrg}

	orgKey, err := s.Keyring.UnwrapOrgKey(orgID, org.WrappedOrgKey)
	if err != nil {
		return nil, nil, nil, err
	}
	defer orgKey.Zero()

	projectID := uuid.NewString()
	projectKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, nil, nil, err
	}
	defer projectKey.Zero()
	wrappedProject, err := crypto.WrapProjectKey(orgKey, orgID, projectID, projectKey)
	if err != nil {
		return nil, nil, nil, err
	}
	project := &store.Project{
		ID:                projectID,
		OrgID:             orgID,
		Slug:              projectSlug,
		Name:              projectName,
		WrappedProjectKey: wrappedProject,
	}
	envs := defaultEnvironments(projectID)
	roles, ownerRoleID := builtinRoles(orgID)

	if err := s.Store.SetupInstance(ctx, user, org, roles, ownerRoleID, project, ptrEnvs(envs)); err != nil {
		return nil, nil, nil, err
	}

	actor := audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name}
	for _, ev := range []struct {
		action   string
		resource audit.Resource
	}{
		{"org.create", audit.Resource{Type: "org", ID: org.ID, Name: org.Slug}},
		{"project.create", audit.Resource{Type: "project", ID: project.ID, Name: project.Slug}},
	} {
		if err := s.record(ctx, actor, org.ID, ev.action, ev.resource, audit.Details{}, AuditMeta{}); err != nil {
			s.Logger.Error("audit setup event failed", "action", ev.action, "error", err)
		}
	}
	return org, project, envs, nil
}

// Slugify turns a display name into a candidate slug.
func Slugify(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 60 {
		out = strings.TrimRight(out[:60], "-")
	}
	return out
}
