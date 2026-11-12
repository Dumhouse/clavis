package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// EnvironmentOverlay is a tenant/region overlay on an environment.
type EnvironmentOverlay struct {
	ID        string
	Slug      string
	Name      string
	CreatedAt time.Time
}

// OverlaySecret is one value in an environment overlay.
type OverlaySecret struct {
	Key       string
	Kind      string
	Value     string
	Version   int
	UpdatedAt time.Time
	Revealed  bool
}

// ListEnvironmentOverlays returns tenant/region overlays for an environment.
func (s *Service) ListEnvironmentOverlays(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]EnvironmentOverlay, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	overlays, err := s.Store.ListEnvironmentOverlays(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	out := make([]EnvironmentOverlay, 0, len(overlays))
	for _, overlay := range overlays {
		out = append(out, EnvironmentOverlay{
			ID: overlay.ID, Slug: overlay.Slug, Name: overlay.Name, CreatedAt: overlay.CreatedAt,
		})
	}
	return out, nil
}

// CreateEnvironmentOverlay adds a tenant/region overlay.
func (s *Service) CreateEnvironmentOverlay(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, slug, name string, meta AuditMeta) (*EnvironmentOverlay, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsManage); err != nil {
		return nil, err
	}
	slug, err = NormalizeSlug(slug)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = slug
	}
	overlay := &store.Overlay{
		ID: uuid.NewString(), EnvironmentID: env.ID, Slug: slug, Name: name, CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateEnvironmentOverlay(ctx, overlay); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "overlay.create",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: slug}, meta); err != nil {
		s.Logger.Error("audit overlay.create failed", "env_id", env.ID, "error", err)
	}
	return &EnvironmentOverlay{ID: overlay.ID, Slug: overlay.Slug, Name: overlay.Name, CreatedAt: overlay.CreatedAt}, nil
}

// ListOverlaySecrets returns overlay values merged-ready.
func (s *Service) ListOverlaySecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug string, reveal bool) ([]OverlaySecret, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	if reveal {
		if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
			return nil, err
		}
	}
	overlay, err := s.Store.GetEnvironmentOverlay(ctx, env.ID, overlaySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	items, err := s.Store.ListOverlayItems(ctx, overlay.ID)
	if err != nil {
		return nil, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	out := make([]OverlaySecret, 0, len(items))
	for i := range items {
		item := &items[i]
		key, _, err := valueKey.Decrypt(item.KeyCiphertext,
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "key", 1))
		if err != nil {
			return nil, err
		}
		view := OverlaySecret{Key: string(key), Kind: item.Kind, Version: item.Version, UpdatedAt: item.UpdatedAt}
		if reveal || item.Kind == "variable" {
			value, _, err := valueKey.Decrypt(item.ValueCiphertext,
				crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", item.Version))
			if err != nil {
				return nil, err
			}
			view.Value = string(value)
			view.Revealed = true
		}
		out = append(out, view)
	}
	if reveal {
		if err := s.record(ctx, toAuditActor(actor), org.ID, "overlay.list_reveal",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug, Count: len(out), Reason: overlay.Slug, Channel: "ui"}, AuditMeta{}); err != nil {
			s.Logger.Error("audit overlay.list_reveal failed", "env_id", env.ID, "error", err)
		}
	}
	return out, nil
}

// SetOverlaySecret upserts one key in a tenant/region overlay.
func (s *Service) SetOverlaySecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug string, in SecretInput, meta AuditMeta) error {
	if err := validateSecretInput(&in, true); err != nil {
		return err
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return err
	}
	overlay, err := s.Store.GetEnvironmentOverlay(ctx, env.ID, overlaySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, in.Key)
	existing, err := s.Store.GetOverlayItemByBlindIndex(ctx, overlay.ID, blind)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if existing != nil {
		next := existing.Version + 1
		valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Value),
			crypto.BindAAD(org.ID, project.ID, env.ID, existing.ID, "value", next))
		if err != nil {
			return err
		}
		existing.Kind = in.Kind
		existing.ValueCiphertext = valueCT
		existing.Version = next
		if err := s.Store.UpdateItem(ctx, existing, "Overlay update", actor.ID); err != nil {
			return err
		}
	} else {
		itemID := uuid.NewString()
		keyCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Key),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "key", 1))
		if err != nil {
			return err
		}
		valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Value),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "value", 1))
		if err != nil {
			return err
		}
		item := &store.Item{
			ID: itemID, EnvironmentID: env.ID, OverlayID: &overlay.ID, Kind: in.Kind,
			KeyCiphertext: keyCT, KeyBlindIndex: blind, ValueCiphertext: valueCT, Version: 1,
		}
		if err := s.Store.CreateItem(ctx, item, "Overlay create"); err != nil {
			return err
		}
	}
	return s.record(ctx, toAuditActor(actor), org.ID, "overlay.set",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: overlay.Slug, Count: 1}, meta)
}

// DeleteOverlaySecret removes a key from a tenant/region overlay.
func (s *Service) DeleteOverlaySecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug, key string, meta AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsDelete); err != nil {
		return err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return err
	}
	overlay, err := s.Store.GetEnvironmentOverlay(ctx, env.ID, overlaySlug)
	if err != nil {
		return ErrNotFound
	}
	_, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer indexKey.Zero()
	item, err := s.Store.GetOverlayItemByBlindIndex(ctx, overlay.ID, crypto.BlindIndex(indexKey, key))
	if err != nil {
		return ErrNotFound
	}
	return s.Store.SoftDeleteItem(ctx, item.ID)
}

// ApplyOverlay merges overlay values over a resolved secret list.
func (s *Service) ApplyOverlay(ctx context.Context, _ authz.Actor, org *store.Org, project *store.Project, env *store.Environment, secrets []Secret, overlaySlug string, reveal bool) ([]Secret, error) {
	if overlaySlug == "" {
		return secrets, nil
	}
	overlay, err := s.Store.GetEnvironmentOverlay(ctx, env.ID, overlaySlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	items, err := s.Store.ListOverlayItems(ctx, overlay.ID)
	if err != nil {
		return nil, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	byKey := make(map[string]int, len(secrets))
	for i := range secrets {
		byKey[secrets[i].Key] = i
	}
	for i := range items {
		item := &items[i]
		key, _, err := valueKey.Decrypt(item.KeyCiphertext,
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "key", 1))
		if err != nil {
			return nil, err
		}
		view := Secret{
			Key: string(key), Kind: item.Kind, Version: item.Version, UpdatedAt: item.UpdatedAt,
			SourceEnvironment: "overlay:" + overlay.Slug, Inherited: true, Overridden: true,
		}
		if reveal || item.Kind == "variable" {
			value, _, err := valueKey.Decrypt(item.ValueCiphertext,
				crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", item.Version))
			if err != nil {
				return nil, err
			}
			view.Value = string(value)
			view.Revealed = true
		}
		if index, ok := byKey[view.Key]; ok {
			secrets[index] = view
		} else {
			byKey[view.Key] = len(secrets)
			secrets = append(secrets, view)
		}
	}
	return secrets, nil
}

func (s *Service) resolveOverlayEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) (*store.Org, *store.Project, *store.Environment, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, nil, nil, err
	}
	return org, project, env, nil
}

// ListSecretsWithOverlay resolves an environment and applies tenant overlays.
func (s *Service) ListSecretsWithOverlay(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug string, reveal bool, channel string, meta AuditMeta) ([]Secret, string, error) {
	secrets, etag, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, envSlug, reveal, channel, meta)
	if err != nil || overlaySlug == "" {
		return secrets, etag, err
	}
	org, project, env, err := s.resolveOverlayEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, "", err
	}
	secrets, err = s.ApplyOverlay(ctx, actor, org, project, env, secrets, overlaySlug, reveal)
	return secrets, etag, err
}

// ResolveSecretsWithOverlay resolves references after applying an overlay.
func (s *Service) ResolveSecretsWithOverlay(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug, channel string, meta AuditMeta) ([]Secret, string, error) {
	secrets, etag, err := s.ListSecretsWithOverlay(ctx, actor, orgSlug, projectSlug, envSlug, overlaySlug, true, channel, meta)
	if err != nil {
		return nil, "", err
	}
	if err := resolveSecretReferences(secrets); err != nil {
		return nil, "", err
	}
	return secrets, etag, nil
}

// OverlaySecretInput is a create/update request for an overlay value.
type OverlaySecretInput struct {
	Value string
	Kind  string
}

// SetOverlayValue is a convenience wrapper around SetOverlaySecret.
func (s *Service) SetOverlayValue(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, overlaySlug, key, value, kind string, meta AuditMeta) error {
	return s.SetOverlaySecret(ctx, actor, orgSlug, projectSlug, envSlug, overlaySlug, SecretInput{
		Key: key, Value: value, Kind: kind, ChangeMessage: "Overlay update",
	}, meta)
}

// GetSecretWithOverlay applies a tenant overlay to one key.
func (s *Service) GetSecretWithOverlay(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key, overlaySlug string, reveal bool, reason, channel string, meta AuditMeta) (*Secret, error) {
	secret, err := s.GetSecret(ctx, actor, orgSlug, projectSlug, envSlug, key, reveal, reason, channel, meta)
	if err != nil || overlaySlug == "" {
		return secret, err
	}
	org, project, env, err := s.resolveOverlayEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	merged, err := s.ApplyOverlay(ctx, actor, org, project, env, []Secret{*secret}, overlaySlug, reveal)
	if err != nil {
		return nil, err
	}
	if len(merged) == 0 || merged[0].Key != key {
		return secret, nil
	}
	return &merged[0], nil
}
