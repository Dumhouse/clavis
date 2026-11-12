package service

import (
	"context"
	"errors"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// ListAuditEvents returns an org's audit events after afterSeq and whether the
// full chain verifies. Only AuditRead holders may call it.
func (s *Service) ListAuditEvents(ctx context.Context, actor authz.Actor, orgSlug string, afterSeq int64, limit int) ([]audit.Event, bool, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, ErrNotFound
		}
		return nil, false, err
	}
	if err := s.Authorize(actor, authz.AuditRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, false, err
	}
	events, err := s.Store.ListAuditEvents(ctx, org.ID, afterSeq, limit)
	if err != nil {
		return nil, false, err
	}
	verified := s.Store.VerifyAuditChain(ctx, org.ID) == nil
	return events, verified, nil
}

// RecordAuthEvent writes an authentication event to each org the user belongs
// to. Authentication is not scoped to one org, so it is recorded everywhere the
// person can see it.
func (s *Service) RecordAuthEvent(ctx context.Context, user *store.User, action, channel string, meta AuditMeta) {
	memberships, err := s.Store.ListOrgsForUser(ctx, user.ID)
	if err != nil {
		s.Logger.Error("record auth event failed", "user_id", user.ID, "action", action, "error", err)
		return
	}
	actor := audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name}
	for _, membership := range memberships {
		if err := s.record(ctx, actor, membership.Org.ID, action,
			audit.Resource{Type: "user", ID: user.ID}, audit.Details{Channel: channel}, meta); err != nil {
			s.Logger.Error("append auth event failed", "org_id", membership.Org.ID, "action", action, "error", err)
		}
	}
}

// AuditLabel is human-readable context for an audit event: the project and
// environment slugs and, for secret events, the decrypted key name. Key names
// are only filled in when the caller may read names in that scope.
type AuditLabel struct {
	Project     string
	Environment string
	Key         string
}

// AuditLabels resolves labels for events. Lookups are cached per request and
// failures leave fields empty: labels are a convenience, never an error.
func (s *Service) AuditLabels(ctx context.Context, actor authz.Actor, orgSlug string, events []audit.Event) map[string]AuditLabel {
	labels := make(map[string]AuditLabel, len(events))
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		return labels
	}
	projects := map[string]*store.Project{}
	envs := map[string]*store.Environment{}
	keys := map[string]crypto.Key{}
	defer func() {
		for _, key := range keys {
			key.Zero()
		}
	}()
	project := func(id string) *store.Project {
		if p, ok := projects[id]; ok {
			return p
		}
		p, err := s.Store.GetProjectByID(ctx, id)
		if err != nil || p.OrgID != org.ID {
			p = nil
		}
		projects[id] = p
		return p
	}
	environment := func(id string) *store.Environment {
		if e, ok := envs[id]; ok {
			return e
		}
		e, err := s.Store.GetEnvironmentByID(ctx, id)
		if err != nil {
			e = nil
		}
		envs[id] = e
		return e
	}
	for _, event := range events {
		var label AuditLabel
		if event.Resource.ProjectID == "" {
			continue
		}
		p := project(event.Resource.ProjectID)
		if p == nil {
			continue
		}
		label.Project = p.Slug
		if event.Resource.EnvID != "" {
			if e := environment(event.Resource.EnvID); e != nil {
				label.Environment = e.Slug
			}
		}
		if event.Resource.Type == "secret" && event.Resource.ID != "" {
			label.Key = s.auditKeyName(ctx, actor, org, p, event.Resource.ID, environment, keys)
		}
		labels[event.ID] = label
	}
	return labels
}

func (s *Service) auditKeyName(ctx context.Context, actor authz.Actor, org *store.Org, project *store.Project, itemID string, environment func(string) *store.Environment, keys map[string]crypto.Key) string {
	item, err := s.Store.GetItemByIDAny(ctx, itemID)
	if err != nil {
		return ""
	}
	env := environment(item.EnvironmentID)
	if env == nil || env.ProjectID != project.ID {
		return ""
	}
	if s.Authorize(actor, authz.SecretsReadNames, secretScope(org, project, env)) != nil {
		return ""
	}
	valueKey, ok := keys[project.ID]
	if !ok {
		var indexKey crypto.Key
		valueKey, indexKey, err = s.ProjectKeys(ctx, org, project)
		if err != nil {
			return ""
		}
		indexKey.Zero()
		keys[project.ID] = valueKey
	}
	name, err := s.decryptKey(valueKey, org, project, env, item)
	if err != nil {
		return ""
	}
	return name
}
