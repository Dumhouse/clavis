package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// AgentAllowlist is an agent identity's server-side command allowlist.
type AgentAllowlist struct {
	IdentityID string
	Name       string
	Commands   []string
}

// GetAgentAllowlist reads an agent's allowlist.
func (s *Service) GetAgentAllowlist(ctx context.Context, actor authz.Actor, orgSlug, identityID string) (*AgentAllowlist, error) {
	org, err := s.tokenOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	identity, err := s.Store.GetIdentity(ctx, org.ID, identityID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &AgentAllowlist{
		IdentityID: identity.ID, Name: identity.Name,
		Commands: parseAllowedCommands(identity.AllowedCommands),
	}, nil
}

// SetAgentAllowlist replaces an agent's allowlist. An empty list is deny-all.
func (s *Service) SetAgentAllowlist(ctx context.Context, actor authz.Actor, orgSlug, identityID string, commands []string, meta AuditMeta) (*AgentAllowlist, error) {
	org, err := s.tokenOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	identity, err := s.Store.GetIdentity(ctx, org.ID, identityID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if identity.Type != "agent" {
		return nil, fmt.Errorf("%w: command allowlists only apply to agent identities", ErrValidation)
	}
	normalized, err := normalizeAgentCommands(commands)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	if err := s.Store.UpdateIdentityAllowedCommands(ctx, org.ID, identity.ID, string(encoded)); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "agent.allowlist",
		audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Count: len(normalized), Commands: normalized}, meta); err != nil {
		s.Logger.Error("audit agent.allowlist failed", "identity_id", identity.ID, "error", err)
	}
	return &AgentAllowlist{IdentityID: identity.ID, Name: identity.Name, Commands: normalized}, nil
}

// CheckAgentCommand rejects a command that is not on the agent's allowlist and
// records the denial. Agents can only check their own identity.
func (s *Service) CheckAgentCommand(ctx context.Context, actor authz.Actor, orgSlug, identityID, command string, meta AuditMeta) error {
	if actor.Kind != authz.SubjectAgent || actor.ID != identityID {
		return ErrForbidden
	}
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	command = strings.TrimSpace(command)
	if command == "" || len(command) > 2048 {
		return fmt.Errorf("%w: command is required and must be under 2048 characters", ErrValidation)
	}
	identity, err := s.Store.GetIdentity(ctx, org.ID, identityID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if identity.Type != "agent" {
		return ErrForbidden
	}
	allowed := parseAllowedCommands(identity.AllowedCommands)
	if matchAgentCommand(allowed, command) {
		return nil
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "agent.command_denied",
		audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Command: command}, meta); err != nil {
		s.Logger.Error("audit agent.command_denied failed", "identity_id", identity.ID, "error", err)
	}
	return ErrAgentCommandDenied
}

// ResolveAgentSecrets reveals values for an allowed command. The command check
// happens before values are decrypted, so a denied command never receives data.
func (s *Service) ResolveAgentSecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, command string, meta AuditMeta) ([]Secret, string, error) {
	if actor.Kind != authz.SubjectAgent {
		return nil, "", ErrForbidden
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, "", err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, "", err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsUse); err != nil {
		return nil, "", err
	}
	if err := s.CheckAgentCommand(ctx, actor, orgSlug, actor.ID, command, meta); err != nil {
		return nil, "", err
	}
	// The allowlist and use permission stand in for read-values for this call.
	augmented := actor
	augmented.Bindings = append([]authz.Binding(nil), actor.Bindings...)
	augmented.Bindings = append(augmented.Bindings, authz.Binding{
		Permissions: []authz.Permission{authz.SecretsReadValues},
		Scope:       authz.Scope{OrgID: org.ID, ProjectID: project.ID, Environment: env.Slug},
	})
	secrets, etag, err := s.ListSecrets(ctx, augmented, orgSlug, projectSlug, envSlug, true, "agent", meta)
	if err != nil {
		return nil, "", err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "agent.command_use",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Command: command, Count: len(secrets), Channel: "agent"}, meta); err != nil {
		s.Logger.Error("audit agent.command_use failed", "env_id", env.ID, "error", err)
	}
	return secrets, etag, nil
}

// RequestSecret records an agent's request for a missing key. It never creates
// or reveals a value; a human still decides what to add.
func (s *Service) RequestSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key, reason string, meta AuditMeta) error {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > maxKeyLength || !keyPattern.MatchString(key) {
		return fmt.Errorf("%w: a valid secret key is required", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 1024 {
		return fmt.Errorf("%w: reason must be under 1024 characters", ErrValidation)
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsUse); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "agent.secret_requested",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, RequestedKey: key, Reason: reason, Channel: "agent"}, meta); err != nil {
		return err
	}
	return nil
}

func parseAllowedCommands(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var commands []string
	if err := json.Unmarshal([]byte(raw), &commands); err != nil {
		return nil
	}
	sort.Strings(commands)
	return commands
}

func normalizeAgentCommands(commands []string) ([]string, error) {
	if len(commands) > 100 {
		return nil, fmt.Errorf("%w: at most 100 command patterns are allowed", ErrValidation)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(commands))
	for _, command := range commands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if len(command) > 256 {
			return nil, fmt.Errorf("%w: command patterns must be under 256 characters", ErrValidation)
		}
		if command == "*" || command == "**" {
			return nil, fmt.Errorf("%w: a bare wildcard would allow every command; list a command prefix instead", ErrValidation)
		}
		for _, r := range command {
			if r < 0x20 || r == 0x7f {
				return nil, fmt.Errorf("%w: command patterns cannot contain control characters", ErrValidation)
			}
		}
		if seen[command] {
			continue
		}
		seen[command] = true
		out = append(out, command)
	}
	sort.Strings(out)
	return out, nil
}

func matchAgentCommand(patterns []string, command string) bool {
	for _, pattern := range patterns {
		if commandGlob(pattern, command) {
			return true
		}
	}
	return false
}

func commandGlob(pattern, value string) bool {
	var b strings.Builder
	b.WriteString("^")
	for _, r := range pattern {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString("$")
	matched, err := regexp.MatchString(b.String(), value)
	return err == nil && matched
}
