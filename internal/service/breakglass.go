package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// BreakGlass grants immediate, loudly-audited access and lifts an active freeze.
func (s *Service) BreakGlass(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, role, reason string, durationSeconds int, meta AuditMeta) (*store.RoleBinding, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, secretScope(org, project, env)); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1024 {
		return nil, fmt.Errorf("%w: break-glass requires a reason under 1024 characters", ErrValidation)
	}
	if durationSeconds <= 0 {
		durationSeconds = 3600
	}
	if durationSeconds < 300 || durationSeconds > 4*3600 {
		return nil, fmt.Errorf("%w: break-glass duration must be between 5 minutes and 4 hours", ErrValidation)
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = authz.RoleAdmin
	}
	roleRow, err := s.Store.GetRoleByName(ctx, org.ID, role)
	if err != nil {
		return nil, fmt.Errorf("%w: unknown role", ErrValidation)
	}
	if freeze, err := s.Store.ActiveFreeze(ctx, env.ID, s.Now().UTC()); err == nil && freeze != nil {
		_ = s.Store.DeleteFreezeWindow(ctx, env.ID, freeze.ID)
	}
	expires := s.Now().UTC().Add(time.Duration(durationSeconds) * time.Second)
	binding := &store.RoleBinding{
		ID: uuid.NewString(), OrgID: org.ID, SubjectKind: "user", SubjectID: actor.ID,
		RoleID: roleRow.ID, RoleName: roleRow.Name, ProjectID: project.ID,
		EnvironmentGlob: env.Slug, PathGlob: "/**", ExpiresAt: &expires,
	}
	if err := s.Store.CreateRoleBinding(ctx, binding); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.break_glass",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: reason, Count: durationSeconds, Channel: "break-glass"}, meta); err != nil {
		s.Logger.Error("audit access.break_glass failed", "binding_id", binding.ID, "error", err)
	}
	s.Logger.Warn("break-glass access granted", "actor", actor.ID, "env", env.Slug, "role", role, "reason", reason)
	return binding, nil
}
