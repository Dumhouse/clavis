package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

var branchSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// EnsureBranchEnvironment creates or refreshes a preview environment for a git
// branch. Branch environments inherit from the first development environment.
func (s *Service) EnsureBranchEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, branch string, meta AuditMeta) (*store.Environment, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" || len(branch) > 255 {
		return nil, fmt.Errorf("%w: branch name is required", ErrValidation)
	}
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return nil, err
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	for i := range envs {
		env := &envs[i]
		if env.PreviewSource == "branch" && env.BranchPattern == branch {
			settings := ParseOrgSettings(org.Settings)
			days := settings.PreviewRetentionDays
			if days <= 0 {
				days = 7
			}
			expires := now.AddDate(0, 0, days)
			env.ExpiresAt = &expires
			if err := s.Store.UpdateEnvironment(ctx, env); err != nil {
				return nil, err
			}
			return env, nil
		}
	}
	var parent *store.Environment
	position := len(envs)
	for i := range envs {
		if envs[i].Type == "development" && parent == nil {
			parent = &envs[i]
		}
		if envs[i].Position >= position {
			position = envs[i].Position + 1
		}
	}
	settings := ParseOrgSettings(org.Settings)
	days := settings.PreviewRetentionDays
	if days <= 0 {
		days = 7
	}
	expires := now.AddDate(0, 0, days)
	slug := branchEnvironmentSlug(branch)
	env := &store.Environment{
		ID: uuid.NewString(), ProjectID: project.ID, Slug: slug,
		Name: "Branch " + branch, Type: "preview", Color: "#8b5cf6", Position: position,
		BranchPattern: branch, PreviewSource: "branch", ExpiresAt: &expires,
	}
	if parent != nil {
		env.ParentID = &parent.ID
	}
	if err := s.Store.CreateEnvironment(ctx, env); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.branch.create",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: branch}, meta); err != nil {
		s.Logger.Error("audit env.branch.create failed", "env_id", env.ID, "error", err)
	}
	s.publish(org.ID, "environments.changed", project, env)
	return env, nil
}

func branchEnvironmentSlug(branch string) string {
	base := branchSlugPattern.ReplaceAllString(strings.ToLower(branch), "-")
	base = strings.Trim(base, "-")
	if len(base) > 36 {
		base = base[:36]
		base = strings.Trim(base, "-")
	}
	if base == "" {
		base = "branch"
	}
	hash := sha256.Sum256([]byte(branch))
	return "dev-" + base + "-" + hex.EncodeToString(hash[:])[:6]
}
