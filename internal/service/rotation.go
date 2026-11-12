package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// RotationPolicyView is policy metadata without provider secrets.
type RotationPolicyView struct {
	ID          string
	Environment string
	SecretKey   string
	Provider    string
	Interval    int
	Enabled     bool
	LastRunAt   *time.Time
	NextRunAt   time.Time
	LastStatus  string
	LastError   string
}

// RotationRunView is one rotation attempt.
type RotationRunView struct {
	ID         string
	Status     string
	Phase      string
	Message    string
	StartedAt  time.Time
	FinishedAt *time.Time
}

// CreateRotationPolicy schedules rotation for one environment key.
func (s *Service) CreateRotationPolicy(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key, provider string, config map[string]any, interval int, enabled bool, meta AuditMeta) (*RotationPolicyView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("%w: secret key is required", ErrValidation)
	}
	if _, err := s.GetSecret(ctx, actor, orgSlug, projectSlug, envSlug, key, false, "", "ui", meta); err != nil {
		return nil, fmt.Errorf("%w: secret %s does not exist in %s", ErrValidation, key, envSlug)
	}
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "postgres", "aws", "webhook":
	default:
		return nil, fmt.Errorf("%w: rotation provider must be postgres, aws, or webhook", ErrValidation)
	}
	if interval < 60 {
		interval = 60
	}
	if interval > 90*24*3600 {
		return nil, fmt.Errorf("%w: rotation interval must be under 90 days", ErrValidation)
	}
	if err := validateRotationConfig(provider, config); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	ciphertext, err := s.encryptOrgSecret(org, id, "rotation_config", string(raw))
	if err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	policy := &store.RotationPolicy{
		ID: id, ProjectID: project.ID, EnvID: env.ID, SecretKey: key, Provider: provider,
		Config: ciphertext, Interval: interval, Enabled: enabled,
		NextRunAt: now.Add(time.Duration(interval) * time.Second), CreatedAt: now,
	}
	if err := s.Store.CreateRotationPolicy(ctx, policy); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "rotation.policy.create",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: provider, Count: 1}, meta); err != nil {
		s.Logger.Error("audit rotation.policy.create failed", "policy_id", policy.ID, "error", err)
	}
	return rotationPolicyView(policy, env.Slug), nil
}

func validateRotationConfig(provider string, config map[string]any) error {
	required := map[string][]string{
		"postgres": {"host", "database", "username", "password", "role"},
		"aws":      {"region", "access_key", "secret_key", "user_name"},
		"webhook":  {"url"},
	}
	for _, key := range required[provider] {
		if value, _ := config[key].(string); strings.TrimSpace(value) == "" {
			return fmt.Errorf("%w: %s rotation needs %s", ErrValidation, provider, key)
		}
	}
	if provider == "webhook" {
		urlValue, _ := config["url"].(string)
		if !strings.HasPrefix(urlValue, "https://") && !strings.HasPrefix(urlValue, "http://") {
			return fmt.Errorf("%w: webhook URL must be http(s)", ErrValidation)
		}
	}
	return nil
}

// ListRotationPolicies returns policies for a project/environment.
func (s *Service) ListRotationPolicies(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]RotationPolicyView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	policies, err := s.Store.ListRotationPolicies(ctx, project.ID, env.ID)
	if err != nil {
		return nil, err
	}
	out := make([]RotationPolicyView, 0, len(policies))
	for i := range policies {
		out = append(out, *rotationPolicyView(&policies[i], env.Slug))
	}
	return out, nil
}

// ListRotationRuns returns recent runs for a policy.
func (s *Service) ListRotationRuns(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, policyID string) ([]RotationRunView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	policy, err := s.Store.GetRotationPolicy(ctx, project.ID, policyID)
	if err != nil {
		return nil, ErrNotFound
	}
	runs, err := s.Store.ListRotationRuns(ctx, policy.ID, 50)
	if err != nil {
		return nil, err
	}
	out := make([]RotationRunView, 0, len(runs))
	for _, run := range runs {
		out = append(out, RotationRunView{
			ID: run.ID, Status: run.Status, Phase: run.Phase, Message: run.Message,
			StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
		})
	}
	return out, nil
}

// RunRotation rotates immediately.
func (s *Service) RunRotation(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, policyID string, meta AuditMeta) (*RotationRunView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	policy, err := s.Store.GetRotationPolicy(ctx, project.ID, policyID)
	if err != nil {
		return nil, ErrNotFound
	}
	run, err := s.rotatePolicy(ctx, org, project, env, policy, toAuditActor(actor), meta)
	if err != nil {
		return nil, err
	}
	return &RotationRunView{ID: run.ID, Status: run.Status, Phase: run.Phase, Message: run.Message,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt}, nil
}

// SetRotationEnabled toggles a policy.
func (s *Service) SetRotationEnabled(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, policyID string, enabled bool, _ AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return err
	}
	policy, err := s.Store.GetRotationPolicy(ctx, project.ID, policyID)
	if err != nil {
		return ErrNotFound
	}
	policy.Enabled = enabled
	if enabled {
		policy.NextRunAt = s.Now().UTC().Add(time.Duration(policy.Interval) * time.Second)
	}
	return s.Store.UpdateRotationPolicy(ctx, policy)
}

// RevokeRotationPolicy deletes a policy.
func (s *Service) RevokeRotationPolicy(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, policyID string, meta AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return err
	}
	if err := s.Store.DeleteRotationPolicy(ctx, project.ID, policyID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "rotation.policy.delete",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Count: 1}, meta); err != nil {
		s.Logger.Error("audit rotation.policy.delete failed", "env_id", env.ID, "error", err)
	}
	return nil
}

// RunDueRotations is called by the background worker.
func (s *Service) RunDueRotations(ctx context.Context) {
	policies, err := s.Store.ListDueRotationPolicies(ctx, s.Now().UTC())
	if err != nil {
		s.Logger.Error("list due rotations failed", "error", err)
		return
	}
	system := audit.Actor{ID: "rotation-worker", Kind: string(authz.SubjectMachine), Name: "rotation-worker"}
	for i := range policies {
		policy := &policies[i]
		project, err := s.Store.GetProjectByID(ctx, policy.ProjectID)
		if err != nil {
			continue
		}
		org, err := s.Store.GetOrgByID(ctx, project.OrgID)
		if err != nil {
			continue
		}
		env, err := s.Store.GetEnvironmentByID(ctx, policy.EnvID)
		if err != nil {
			continue
		}
		if _, err := s.rotatePolicy(ctx, org, project, env, policy, system, AuditMeta{}); err != nil {
			s.Logger.Warn("rotation failed", "policy_id", policy.ID, "error", err)
		}
	}
}

func (s *Service) rotatePolicy(ctx context.Context, org *store.Org, project *store.Project, env *store.Environment, policy *store.RotationPolicy, actor audit.Actor, meta AuditMeta) (*store.RotationRun, error) {
	run := &store.RotationRun{
		ID: uuid.NewString(), PolicyID: policy.ID, Status: "running", Phase: "create",
		StartedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateRotationRun(ctx, run); err != nil {
		return nil, err
	}
	finish := func(status, phase, message string) {
		run.Status, run.Phase, run.Message = status, phase, message
		now := s.Now().UTC()
		run.FinishedAt = &now
		_ = s.Store.FinishRotationRun(ctx, run.ID, status, phase, message, now)
		policy.LastRunAt = &now
		policy.NextRunAt = now.Add(time.Duration(policy.Interval) * time.Second)
		policy.LastStatus, policy.LastError = status, message
		_ = s.Store.UpdateRotationPolicy(ctx, policy)
		if status == "failed" {
			if err := s.record(ctx, actor, org.ID, "rotation.failed",
				audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
				audit.Details{Environment: env.Slug, Reason: policy.SecretKey, Channel: policy.Provider}, meta); err != nil {
				s.Logger.Error("audit rotation.failed failed", "policy_id", policy.ID, "error", err)
			}
		}
	}
	current, err := s.GetSecret(ctx, systemActor(org, project, env), org.Slug, project.Slug, env.Slug, policy.SecretKey, true, "rotation", "worker", meta)
	if err != nil {
		finish("failed", "read", err.Error())
		return run, err
	}
	rawConfig, err := s.decryptOrgSecret(org, policy.ID, "rotation_config", policy.Config)
	if err != nil {
		finish("failed", "config", err.Error())
		return run, err
	}
	result, err := s.rotateCreate(ctx, policy.Provider, rawConfig, current.Value)
	if err != nil {
		finish("failed", "create", err.Error())
		return run, err
	}
	runner := systemActor(org, project, env)
	in := SecretInput{Key: policy.SecretKey, Value: result.Value, Kind: current.Kind,
		Comment: current.Comment, Folder: current.Folder, Notes: current.Notes,
		ProviderURL: current.ProviderURL, OwnerGroupID: current.OwnerGroupID,
		Tags: current.Tags, ChangeMessage: "Rotated by policy " + policy.ID}
	if _, err := s.UpdateSecret(ctx, runner, org.Slug, project.Slug, env.Slug, policy.SecretKey, in, meta); err != nil {
		if errors.Is(err, ErrNotFound) {
			_, err = s.CreateSecret(ctx, runner, org.Slug, project.Slug, env.Slug, in, meta)
		}
		if err != nil {
			_ = s.rotateRollback(ctx, policy.Provider, rawConfig, current.Value, result)
			finish("failed", "write", err.Error())
			return run, err
		}
	}
	verified, err := s.GetSecret(ctx, runner, org.Slug, project.Slug, env.Slug, policy.SecretKey, true, "rotation", "worker", meta)
	if err != nil || verified.Value != result.Value {
		_ = s.rotateRollback(ctx, policy.Provider, rawConfig, current.Value, result)
		finish("failed", "verify", "new value did not verify")
		return run, fmt.Errorf("%w: rotated value did not verify", ErrValidation)
	}
	_ = s.rotateRevokeOld(ctx, policy.Provider, rawConfig, current.Value, result)
	finish("success", "revoke", "old credential revoked")
	return run, nil
}

func systemActor(org *store.Org, project *store.Project, env *store.Environment) authz.Actor {
	return authz.Actor{
		ID: "rotation-worker", Kind: authz.SubjectMachine, Name: "rotation-worker",
		Bindings: []authz.Binding{{
			Role:  authz.RoleOwner,
			Scope: authz.Scope{OrgID: org.ID, ProjectID: project.ID, Environment: env.Slug},
		}},
	}
}

func rotationPolicyView(policy *store.RotationPolicy, envSlug string) *RotationPolicyView {
	return &RotationPolicyView{
		ID: policy.ID, Environment: envSlug, SecretKey: policy.SecretKey, Provider: policy.Provider,
		Interval: policy.Interval, Enabled: policy.Enabled, LastRunAt: policy.LastRunAt,
		NextRunAt: policy.NextRunAt, LastStatus: policy.LastStatus, LastError: policy.LastError,
	}
}

// RotationStatusSecret is an unmanaged secret and its age.
type RotationStatusSecret struct {
	Key            string
	OwnerGroupID   string
	OwnerGroupName string
	UpdatedAt      time.Time
	AgeDays        int
	Stale          bool
}

// RotationStatus summarizes managed and manual-rotation state for an environment.
type RotationStatus struct {
	Environment string
	StaleDays   int
	Managed     int
	Unmanaged   int
	Stale       []RotationStatusSecret
}

// RotationStatusForEnv reports secrets without a rotation policy.
func (s *Service) RotationStatusForEnv(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, staleDays int) (*RotationStatus, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	if staleDays <= 0 {
		staleDays = 90
	}
	secrets, _, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, envSlug, false, "ui", metaEmpty())
	if err != nil {
		return nil, err
	}
	policies, err := s.Store.ListRotationPolicies(ctx, project.ID, env.ID)
	if err != nil {
		return nil, err
	}
	managed := make(map[string]bool, len(policies))
	for _, policy := range policies {
		managed[policy.SecretKey] = true
	}
	status := &RotationStatus{Environment: env.Slug, StaleDays: staleDays, Managed: len(managed)}
	now := s.Now().UTC()
	for _, secret := range secrets {
		if managed[secret.Key] || secret.Kind == "file" {
			continue
		}
		status.Unmanaged++
		age := int(now.Sub(secret.UpdatedAt).Hours() / 24)
		if age >= staleDays {
			status.Stale = append(status.Stale, RotationStatusSecret{
				Key: secret.Key, OwnerGroupID: secret.OwnerGroupID,
				OwnerGroupName: secret.OwnerGroupName, UpdatedAt: secret.UpdatedAt,
				AgeDays: age, Stale: true,
			})
		}
	}
	return status, nil
}

// ManualRotationReminder is a stale unmanaged secret plus recipients.
type ManualRotationReminder struct {
	OrgSlug     string
	ProjectSlug string
	EnvSlug     string
	Key         string
	AgeDays     int
	Emails      []string
}

// ListManualRotationReminders finds stale secrets without rotation policies.
func (s *Service) ListManualRotationReminders(ctx context.Context, staleDays int) []ManualRotationReminder {
	if staleDays <= 0 {
		staleDays = 90
	}
	orgs, err := s.Store.ListAllOrgs(ctx)
	if err != nil {
		return nil
	}
	var out []ManualRotationReminder
	for i := range orgs {
		org := &orgs[i]
		actor := authz.Actor{
			ID: "rotation-reminder", Kind: authz.SubjectMachine, Name: "rotation-reminder",
			Bindings: []authz.Binding{{Role: authz.RoleOwner, Scope: authz.Scope{OrgID: org.ID}}},
		}
		projects, err := s.Store.ListProjects(ctx, org.ID)
		if err != nil {
			continue
		}
		for p := range projects {
			project := &projects[p]
			envs, err := s.Store.ListEnvironments(ctx, project.ID)
			if err != nil {
				continue
			}
			for e := range envs {
				env := &envs[e]
				status, err := s.RotationStatusForEnv(ctx, actor, org.Slug, project.Slug, env.Slug, staleDays)
				if err != nil {
					continue
				}
				for _, secret := range status.Stale {
					emails := []string{}
					if secret.OwnerGroupID != "" {
						members, err := s.Store.ListGroupMembers(ctx, secret.OwnerGroupID)
						if err == nil {
							for _, member := range members {
								emails = append(emails, member.Email)
							}
						}
					}
					if len(emails) == 0 {
						continue
					}
					out = append(out, ManualRotationReminder{
						OrgSlug: org.Slug, ProjectSlug: project.Slug, EnvSlug: env.Slug,
						Key: secret.Key, AgeDays: secret.AgeDays, Emails: emails,
					})
				}
			}
		}
	}
	return out
}

func metaEmpty() AuditMeta { return AuditMeta{} }
