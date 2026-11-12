package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// AgentSessionTTL is the lifetime of a delegated agent session. `clavis mcp`
// renews it before it expires while the person's own login stays valid.
const AgentSessionTTL = time.Hour

// Parent credential kinds for delegated agent sessions.
const (
	ParentSession = "session"
	ParentPAT     = "pat"
)

// AgentSessionParent is the person's credential that started an agent session.
// Revoking it (logging out, deleting the token) ends the agent session.
type AgentSessionParent struct {
	Kind string
	ID   string
}

// AgentSession is a short-lived agent token held only in the memory of the
// process that started it.
type AgentSession struct {
	Token       string
	IdentityID  string
	Name        string
	ProjectID   string
	Environment string
	ExpiresAt   time.Time
}

// StartAgentSession mints a delegated, short-lived token for an agent
// identity. The token never needs to be written to a file: `clavis mcp
// --agent NAME` requests it with the person's own login and keeps it in
// memory. A person can only start an agent with access they hold themselves.
func (s *Service) StartAgentSession(ctx context.Context, actor authz.Actor, parent AgentSessionParent, orgSlug, identityRef string, meta AuditMeta) (*AgentSession, error) {
	if actor.Kind != authz.SubjectUser {
		return nil, fmt.Errorf("%w: only people can start agent sessions", ErrForbidden)
	}
	if parent.ID == "" || (parent.Kind != ParentSession && parent.Kind != ParentPAT) {
		return nil, fmt.Errorf("%w: sign in with a session or personal access token", ErrForbidden)
	}
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	identity, err := s.Store.GetIdentity(ctx, org.ID, identityRef)
	if errors.Is(err, store.ErrNotFound) {
		identity, err = s.Store.GetIdentityByName(ctx, org.ID, identityRef)
	}
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if identity.Type != "agent" {
		return nil, fmt.Errorf("%w: %s is not an agent identity", ErrValidation, identity.Name)
	}

	if err := s.Store.DeleteExpiredAgentSessions(ctx, identity.ID, s.Now().UTC()); err != nil {
		s.Logger.Warn("prune agent sessions failed", "identity_id", identity.ID, "error", err)
	}
	existing, err := s.Store.ListTokensForIdentity(ctx, identity.ID)
	if err != nil {
		return nil, err
	}
	if len(existing) == 0 || existing[0].Scopes == "" {
		return nil, fmt.Errorf("%w: agent %s has no access scope; rotate its token in Identities", ErrValidation, identity.Name)
	}
	var scopes tokenScopes
	if err := json.Unmarshal([]byte(existing[0].Scopes), &scopes); err != nil {
		return nil, fmt.Errorf("service: decode token scopes: %w", err)
	}
	scopes = scopes.withoutDelegation()
	scopes.Role = "Agent"
	scopes.Permissions = []string{string(authz.SecretsReadNames), string(authz.SecretsUse)}

	scope := authz.Scope{OrgID: org.ID, ProjectID: scopes.ProjectID, Environment: scopes.Environment}
	for _, perm := range []authz.Permission{authz.SecretsReadNames, authz.SecretsUse} {
		if !s.Engine.Authorize(actor, perm, scope, s.Now()).Allowed {
			return nil, fmt.Errorf("%w: you can only start an agent with access you hold yourself", ErrForbidden)
		}
	}

	scopes.DelegatedBy = actor.ID
	scopes.ParentKind = parent.Kind
	scopes.ParentID = parent.ID
	token, record, err := s.newToken(ctx, identity, scopes, AgentSessionTTL)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "agent.session_start",
		audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Environment: scopes.Environment, Channel: "mcp"}, meta); err != nil {
		s.Logger.Error("audit agent.session_start failed", "identity_id", identity.ID, "error", err)
	}
	return &AgentSession{
		Token: token, IdentityID: identity.ID, Name: identity.Name,
		ProjectID: scopes.ProjectID, Environment: scopes.Environment, ExpiresAt: *record.ExpiresAt,
	}, nil
}

// delegationActive reports whether the person behind a delegated agent token
// is still an active member and their parent credential is still valid.
func (s *Service) delegationActive(ctx context.Context, orgID string, scopes tokenScopes) bool {
	membership, err := s.Store.GetMembership(ctx, orgID, scopes.DelegatedBy)
	if err != nil || membership.RemovedAt != nil || membership.Status != "active" {
		return false
	}
	active, err := s.Store.CredentialActive(ctx, scopes.ParentKind, scopes.ParentID, scopes.DelegatedBy, s.Now().UTC())
	if err != nil {
		s.Logger.Warn("check agent session parent failed", "error", err)
		return false
	}
	return active
}

func (t tokenScopes) withoutDelegation() tokenScopes {
	t.DelegatedBy, t.ParentKind, t.ParentID = "", "", ""
	return t
}
