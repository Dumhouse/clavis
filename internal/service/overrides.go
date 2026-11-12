package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// SetOverride stores a personal, non-production value for an existing key.
func (s *Service) SetOverride(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, in SecretInput, meta AuditMeta) (*Secret, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if env.Type == "production" {
		return nil, fmt.Errorf("%w: personal overrides are not allowed in production", ErrValidation)
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	in.Key = key
	if err := validateSecretInput(&in, false); err != nil {
		return nil, err
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	chain, err := s.environmentChain(ctx, env)
	if err != nil {
		return nil, err
	}
	var base *store.Item
	for _, candidate := range chain {
		found, err := s.Store.GetItemByBlindIndex(ctx, candidate.ID, blind)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		base = found
		break
	}
	if base == nil {
		return nil, ErrNotFound
	}
	overlay, err := s.Store.GetOrCreateOverlay(ctx, env.ID, actor.ID, actor.Name)
	if err != nil {
		return nil, err
	}
	existing, err := s.Store.GetOverlayItemByBlindIndex(ctx, overlay.ID, blind)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if existing != nil {
		next := existing.Version + 1
		valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Value),
			crypto.BindAAD(org.ID, project.ID, env.ID, existing.ID, "value", next))
		if err != nil {
			return nil, err
		}
		existing.ValueCiphertext = valueCT
		existing.Kind = base.Kind
		existing.Version = next
		if err := s.Store.UpdateItem(ctx, existing, "Personal override", actor.ID); err != nil {
			return nil, err
		}
	} else {
		itemID := uuid.NewString()
		keyCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(key),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "key", 1))
		if err != nil {
			return nil, err
		}
		valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Value),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "value", 1))
		if err != nil {
			return nil, err
		}
		overlayID := overlay.ID
		item := &store.Item{
			ID: itemID, EnvironmentID: env.ID, OverlayID: &overlayID, Kind: base.Kind,
			KeyCiphertext: keyCT, KeyBlindIndex: blind, ValueCiphertext: valueCT,
			Tags: "[]", Version: 1,
		}
		if err := s.Store.CreateItem(ctx, item, "Personal override"); err != nil {
			return nil, err
		}
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.override.set",
		audit.Resource{Type: "secret", ID: base.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit secret.override.set failed", "item_id", base.ID, "error", err)
	}
	return &Secret{Key: key, Kind: base.Kind, Value: in.Value, Revealed: true, Overridden: true}, nil
}

// DeleteOverride removes a personal override.
func (s *Service) DeleteOverride(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, meta AuditMeta) error {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return err
	}
	if env.Type == "production" {
		return fmt.Errorf("%w: personal overrides are not allowed in production", ErrValidation)
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return err
	}
	_, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer indexKey.Zero()
	overlay, err := s.Store.GetOverlay(ctx, env.ID, actor.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetOverlayItemByBlindIndex(ctx, overlay.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.SoftDeleteItem(ctx, item.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.override.delete",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit secret.override.delete failed", "item_id", item.ID, "error", err)
	}
	return nil
}

// loadOverrides returns the actor's override items keyed by plaintext key.
func (s *Service) loadOverrides(ctx context.Context, valueKey crypto.Key, org *store.Org, project *store.Project, env *store.Environment, userID string) (map[string]*store.Item, error) {
	overlay, err := s.Store.GetOverlay(ctx, env.ID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	items, err := s.Store.ListOverlayItems(ctx, overlay.ID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*store.Item, len(items))
	for i := range items {
		item := &items[i]
		key, err := s.decryptKey(valueKey, org, project, env, item)
		if err != nil {
			return nil, err
		}
		out[key] = item
	}
	return out, nil
}

func overrideValue(valueKey crypto.Key, org *store.Org, project *store.Project, env *store.Environment, item *store.Item) (string, error) {
	if len(item.ValueCiphertext) == 0 {
		return "", nil
	}
	value, err := decryptField(valueKey, org, project, env, item, "value", item.Version, item.ValueCiphertext)
	if err != nil {
		return "", err
	}
	return string(value), nil
}
