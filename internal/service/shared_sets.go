package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

// SharedSetView is a set plus item metadata.
type SharedSetView struct {
	Set     *store.SharedSet
	Items   []SharedSetItemView
	Imports []store.SharedSetImport
}

// SharedSetItemView is a decrypted set item.
type SharedSetItemView struct {
	Key       string
	Kind      string
	Comment   string
	Value     string
	Version   int
	UpdatedAt time.Time
}

type sharedValue struct {
	Value   string
	Kind    string
	SetSlug string
}

// CreateSharedSet creates an org-level set with its own key.
func (s *Service) CreateSharedSet(ctx context.Context, actor authz.Actor, orgSlug, name, rawSlug string, meta AuditMeta) (*store.SharedSet, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: shared set name is required", ErrValidation)
	}
	if rawSlug == "" {
		rawSlug = name
	}
	slug, err := NormalizeSlug(rawSlug)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	wrapped, setKey, err := s.Keyring.NewSharedSetKey(org.ID, id)
	if err != nil {
		return nil, err
	}
	defer setKey.Zero()
	set := &store.SharedSet{ID: id, OrgID: org.ID, Slug: slug, Name: name, WrappedKey: wrapped, KeyVersion: 1}
	if err := s.Store.CreateSharedSet(ctx, set); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.create",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit set.create failed", "set_id", set.ID, "error", err)
	}
	return set, nil
}

// ListSharedSets returns sets with item metadata (no values).
func (s *Service) ListSharedSets(ctx context.Context, actor authz.Actor, orgSlug string) ([]SharedSetView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	sets, err := s.Store.ListSharedSets(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SharedSetView, 0, len(sets))
	for i := range sets {
		items, err := s.listSharedSetItems(ctx, org, &sets[i], false)
		if err != nil {
			return nil, err
		}
		imports, err := s.Store.ListSharedSetImports(ctx, sets[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, SharedSetView{Set: &sets[i], Items: items, Imports: imports})
	}
	return out, nil
}

// GetSharedSet returns one set with items, optionally revealing values.
func (s *Service) GetSharedSet(ctx context.Context, actor authz.Actor, orgSlug, slug string, reveal bool) (*SharedSetView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if reveal {
		if err := s.Authorize(actor, authz.SecretsReadValues, authz.Scope{OrgID: org.ID}); err != nil {
			return nil, err
		}
	}
	items, err := s.listSharedSetItems(ctx, org, set, reveal)
	if err != nil {
		return nil, err
	}
	imports, err := s.Store.ListSharedSetImports(ctx, set.ID)
	if err != nil {
		return nil, err
	}
	return &SharedSetView{Set: set, Items: items, Imports: imports}, nil
}

// DeleteSharedSet removes a set and its imports.
func (s *Service) DeleteSharedSet(ctx context.Context, actor authz.Actor, orgSlug, slug string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.DeleteSharedSet(ctx, org.ID, set.ID); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.delete",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit set.delete failed", "set_id", set.ID, "error", err)
	}
	return nil
}

// SetSharedSetItem creates or updates one value in a set.
func (s *Service) SetSharedSetItem(ctx context.Context, actor authz.Actor, orgSlug, slug, key string, in SecretInput, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	in.Key = key
	if err := validateSecretInput(&in, true); err != nil {
		return err
	}
	setKey, err := s.Keyring.UnwrapSharedSetKey(org.ID, set.ID, set.WrappedKey)
	if err != nil {
		return err
	}
	defer setKey.Zero()
	valueKey, err := crypto.DeriveValueKey(setKey)
	if err != nil {
		return err
	}
	defer valueKey.Zero()
	indexKey, err := crypto.DeriveBlindIndexKey(setKey)
	if err != nil {
		return err
	}
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	existing, err := s.Store.GetSharedSetItemByBlindIndex(ctx, set.ID, blind)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	itemID := uuid.NewString()
	version := 1
	if existing != nil {
		itemID = existing.ID
		version = existing.Version + 1
	}
	keyCT, err := valueKey.Encrypt(keyVersion(set.KeyVersion), crypto.Secret(key),
		crypto.BindAAD(org.ID, set.ID, "", itemID, "key", 1))
	if err != nil {
		return err
	}
	valueCT, err := valueKey.Encrypt(keyVersion(set.KeyVersion), crypto.Secret(in.Value),
		crypto.BindAAD(org.ID, set.ID, "", itemID, "value", version))
	if err != nil {
		return err
	}
	var commentCT []byte
	if in.Comment != "" {
		commentCT, err = valueKey.Encrypt(keyVersion(set.KeyVersion), crypto.Secret(in.Comment),
			crypto.BindAAD(org.ID, set.ID, "", itemID, "comment", version))
		if err != nil {
			return err
		}
	}
	item := &store.SharedSetItem{
		ID: itemID, SetID: set.ID, KeyCiphertext: keyCT, KeyBlindIndex: blind,
		ValueCiphertext: valueCT, CommentCiphertext: commentCT, Kind: in.Kind, Version: version,
	}
	if err := s.Store.UpsertSharedSetItem(ctx, item); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.item.set",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug},
		audit.Details{KeyBlindIndex: blind, Version: version}, meta); err != nil {
		s.Logger.Error("audit set.item.set failed", "set_id", set.ID, "error", err)
	}
	return nil
}

// DeleteSharedSetItem removes a value from a set.
func (s *Service) DeleteSharedSetItem(ctx context.Context, actor authz.Actor, orgSlug, slug, key string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	setKey, err := s.Keyring.UnwrapSharedSetKey(org.ID, set.ID, set.WrappedKey)
	if err != nil {
		return err
	}
	defer setKey.Zero()
	indexKey, err := crypto.DeriveBlindIndexKey(setKey)
	if err != nil {
		return err
	}
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetSharedSetItemByBlindIndex(ctx, set.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.DeleteSharedSetItem(ctx, set.ID, item.ID); err != nil {
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.item.delete",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug},
		audit.Details{KeyBlindIndex: blind}, meta); err != nil {
		s.Logger.Error("audit set.item.delete failed", "set_id", set.ID, "error", err)
	}
	return nil
}

// ImportSharedSet imports a set into an environment.
func (s *Service) ImportSharedSet(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, slug string, priority int, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.ImportSharedSet(ctx, set.ID, env.ID, priority); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrConflict
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.import",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit set.import failed", "set_id", set.ID, "error", err)
	}
	return nil
}

// DetachSharedSet removes a set from an environment.
func (s *Service) DetachSharedSet(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, slug string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsManage)
	if err != nil {
		return err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	set, err := s.Store.GetSharedSet(ctx, org.ID, slug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.DetachSharedSet(ctx, set.ID, env.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "set.detach",
		audit.Resource{Type: "shared_set", ID: set.ID, Name: set.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit set.detach failed", "set_id", set.ID, "error", err)
	}
	return nil
}

func (s *Service) listSharedSetItems(ctx context.Context, org *store.Org, set *store.SharedSet, reveal bool) ([]SharedSetItemView, error) {
	items, err := s.Store.ListSharedSetItems(ctx, set.ID)
	if err != nil {
		return nil, err
	}
	setKey, err := s.Keyring.UnwrapSharedSetKey(org.ID, set.ID, set.WrappedKey)
	if err != nil {
		return nil, err
	}
	defer setKey.Zero()
	valueKey, err := crypto.DeriveValueKey(setKey)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	out := make([]SharedSetItemView, 0, len(items))
	for i := range items {
		item := &items[i]
		key, _, err := valueKey.Decrypt(item.KeyCiphertext, crypto.BindAAD(org.ID, set.ID, "", item.ID, "key", 1))
		if err != nil {
			return nil, err
		}
		view := SharedSetItemView{Key: string(key), Kind: item.Kind, Version: item.Version, UpdatedAt: item.UpdatedAt}
		if len(item.CommentCiphertext) > 0 {
			comment, _, err := valueKey.Decrypt(item.CommentCiphertext,
				crypto.BindAAD(org.ID, set.ID, "", item.ID, "comment", item.Version))
			if err == nil {
				view.Comment = string(comment)
			}
		}
		if reveal {
			value, _, err := valueKey.Decrypt(item.ValueCiphertext,
				crypto.BindAAD(org.ID, set.ID, "", item.ID, "value", item.Version))
			if err != nil {
				return nil, err
			}
			view.Value = string(value)
		}
		out = append(out, view)
	}
	return out, nil
}

// sharedSetValues resolves all sets imported by an environment. Later
// priorities override earlier ones.
func (s *Service) sharedSetValues(ctx context.Context, org *store.Org, env *store.Environment) (map[string]sharedValue, error) {
	imports, err := s.Store.ListEnvironmentSharedSets(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	out := map[string]sharedValue{}
	for _, imported := range imports {
		set, err := s.Store.GetSharedSet(ctx, org.ID, imported.SetSlug)
		if err != nil {
			continue
		}
		setKey, err := s.Keyring.UnwrapSharedSetKey(org.ID, set.ID, set.WrappedKey)
		if err != nil {
			return nil, err
		}
		valueKey, err := crypto.DeriveValueKey(setKey)
		setKey.Zero()
		if err != nil {
			return nil, err
		}
		items, err := s.Store.ListSharedSetItems(ctx, set.ID)
		if err != nil {
			valueKey.Zero()
			return nil, err
		}
		for i := range items {
			item := &items[i]
			key, _, err := valueKey.Decrypt(item.KeyCiphertext, crypto.BindAAD(org.ID, set.ID, "", item.ID, "key", 1))
			if err != nil {
				valueKey.Zero()
				return nil, err
			}
			value, _, err := valueKey.Decrypt(item.ValueCiphertext,
				crypto.BindAAD(org.ID, set.ID, "", item.ID, "value", item.Version))
			if err != nil {
				valueKey.Zero()
				return nil, err
			}
			out[string(key)] = sharedValue{Value: string(value), Kind: item.Kind, SetSlug: set.Slug}
		}
		valueKey.Zero()
	}
	return out, nil
}

func expandSetValue(value string, sets map[string]sharedValue) string {
	if len(sets) == 0 {
		return value
	}
	return referenceSetPattern.ReplaceAllStringFunc(value, func(match string) string {
		sub := referenceSetPattern.FindStringSubmatch(match)
		if replacement, ok := sets[sub[1]]; ok {
			return replacement.Value
		}
		return match
	})
}

func applySharedSetReferences(secrets []Secret, sets map[string]sharedValue) {
	if len(sets) == 0 {
		return
	}
	pattern := referenceSetPattern
	for i := range secrets {
		secrets[i].Value = pattern.ReplaceAllStringFunc(secrets[i].Value, func(match string) string {
			sub := pattern.FindStringSubmatch(match)
			if value, ok := sets[sub[1]]; ok {
				return value.Value
			}
			return match
		})
	}
}

func sortedSharedKeys(values map[string]sharedValue) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ListEnvironmentSharedSets returns sets imported by an environment.
func (s *Service) ListEnvironmentSharedSets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]store.SharedSetImport, error) {
	_, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsRead)
	if err != nil {
		return nil, err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.Store.ListEnvironmentSharedSets(ctx, env.ID)
}
