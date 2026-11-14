package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// Statuses CompareSecretValues reports per key.
const (
	CompareNew     = "new"
	CompareChanged = "changed"
	CompareSame    = "same"
)

// CompareSecretValues reports whether each given value differs from the
// environment's current value (its own, inherited, or from a shared set),
// without returning any value. The import preview uses it: the secret list
// carries no secret values, so the browser can't tell a changed value from an
// unchanged one.
//
// An equality check reads the value, so it needs the same permission as
// revealing it, and each call is audited (key count only).
func (s *Service) CompareSecretValues(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, values map[string]string, meta AuditMeta) (map[string]string, error) {
	secrets, _, err := s.listSecrets(ctx, actor, orgSlug, projectSlug, envSlug, true, false, "ui", meta)
	if err != nil {
		return nil, err
	}
	current := make(map[string]string, len(secrets))
	for i := range secrets {
		current[secrets[i].Key] = secrets[i].Value
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		existing, ok := current[key]
		switch {
		case !ok:
			out[key] = CompareNew
		case existing == value:
			out[key] = CompareSame
		default:
			out[key] = CompareChanged
		}
	}

	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.compare",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Count: len(values), Channel: "ui"}, meta); err != nil {
		s.Logger.Error("audit secret.compare failed", "env_id", env.ID, "error", err)
	}
	return out, nil
}

// RevealSecretVersion returns the value of one past version of a key in an
// environment, so the history can be previewed before restoring it. It is a
// reveal: it needs read-values permission, a reason in production, and is
// audited as secret.reveal with the version number.
func (s *Service) RevealSecretVersion(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, version int, reason string, meta AuditMeta) (string, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return "", err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
		return "", err
	}
	if env.Type == "production" && strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("%w: a reason is required to reveal values in production", ErrValidation)
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return "", err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetItemByBlindIndex(ctx, env.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	old, err := s.Store.GetItemVersion(ctx, item.ID, version)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	plaintext, _, err := valueKey.Decrypt(old.ValueCiphertext,
		crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", version))
	if err != nil {
		return "", fmt.Errorf("service: decrypt version %d: %w", version, err)
	}
	value := string(plaintext)
	plaintext.Zero()
	if item.Kind != "variable" {
		if err := s.Store.InsertRevealEvent(ctx, uuid.NewString(), actor.ID, item.ID, "ui", version); err != nil {
			s.Logger.Error("record reveal event failed", "item_id", item.ID, "error", err)
		}
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.reveal",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug, Version: version, Reason: reason, Channel: "ui"}, meta); err != nil {
		s.Logger.Error("audit secret.reveal failed", "item_id", item.ID, "error", err)
	}
	return value, nil
}
