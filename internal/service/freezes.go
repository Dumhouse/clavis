package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// FreezeView is a scheduled environment freeze.
type FreezeView struct {
	ID        string
	StartsAt  time.Time
	EndsAt    time.Time
	Reason    string
	CreatedBy string
	Active    bool
}

// ListFreezes returns upcoming and active freeze windows.
func (s *Service) ListFreezes(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]FreezeView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsRead); err != nil {
		return nil, err
	}
	freezes, err := s.Store.ListFreezeWindows(ctx, env.ID, s.Now().UTC())
	if err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	out := make([]FreezeView, 0, len(freezes))
	for _, freeze := range freezes {
		out = append(out, FreezeView{
			ID: freeze.ID, StartsAt: freeze.StartsAt, EndsAt: freeze.EndsAt,
			Reason: freeze.Reason, CreatedBy: freeze.CreatedBy,
			Active: !now.Before(freeze.StartsAt) && !now.After(freeze.EndsAt),
		})
	}
	return out, nil
}

// CreateFreeze schedules a freeze that blocks writes, promotions, and syncs.
func (s *Service) CreateFreeze(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, starts, ends time.Time, reason string, meta AuditMeta) (*FreezeView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsManage); err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	if starts.IsZero() {
		starts = now
	}
	if ends.IsZero() || !ends.After(starts) {
		return nil, fmt.Errorf("%w: freeze end must be after the start", ErrValidation)
	}
	if ends.Sub(starts) > 90*24*time.Hour {
		return nil, fmt.Errorf("%w: freeze windows are limited to 90 days", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 512 {
		return nil, fmt.Errorf("%w: a freeze reason under 512 characters is required", ErrValidation)
	}
	freeze := &store.FreezeWindow{
		ID: uuid.NewString(), EnvironmentID: env.ID, StartsAt: starts, EndsAt: ends,
		Reason: reason, CreatedBy: actor.ID,
	}
	if err := s.Store.CreateFreezeWindow(ctx, freeze); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.freeze",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: reason}, meta); err != nil {
		s.Logger.Error("audit env.freeze failed", "freeze_id", freeze.ID, "error", err)
	}
	s.publish(org.ID, "environments.changed", project, env)
	return &FreezeView{ID: freeze.ID, StartsAt: starts, EndsAt: ends, Reason: reason,
		CreatedBy: actor.ID, Active: !now.Before(starts)}, nil
}

// DeleteFreeze removes a freeze window.
func (s *Service) DeleteFreeze(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, freezeID string, meta AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsManage); err != nil {
		return err
	}
	if err := s.Store.DeleteFreezeWindow(ctx, env.ID, freezeID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.unfreeze",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit env.unfreeze failed", "freeze_id", freezeID, "error", err)
	}
	s.publish(org.ID, "environments.changed", project, env)
	return nil
}
