package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

const (
	maxKeyLength     = 256
	maxValueBytes    = 64 * 1024
	maxFileBytes     = 1024 * 1024
	maxCommentLength = 4096
	maxMessageLength = 512
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

// Secret is a decrypted view of an item. Value is empty unless the caller asked
// for and was allowed to reveal it.
type Secret struct {
	ItemID            string
	Key               string
	Kind              string
	Comment           string
	Value             string
	Folder            string
	Notes             string
	ProviderURL       string
	OwnerGroupID      string
	OwnerGroupName    string
	SourceEnvironment string
	Inherited         bool
	Overridden        bool
	Tags              []string
	Version           int
	UpdatedAt         time.Time
	ExpiresAt         *time.Time
	Revealed          bool
}

// SecretInput is a create or update request.
type SecretInput struct {
	Key           string
	Value         string
	Kind          string
	Comment       string
	Folder        string
	Notes         string
	ProviderURL   string
	OwnerGroupID  string
	Tags          []string
	ExpiresAt     *time.Time
	ChangeMessage string
	// KeepValue re-encrypts the current value instead of Value, so metadata
	// can be edited without the caller ever seeing the plaintext.
	KeepValue bool
}

// SecretVersion is one entry in a secret's history.
type SecretVersion struct {
	Version       int
	Comment       string
	ChangeMessage string
	ActorID       string
	CreatedAt     time.Time
}

func validateSecretInput(in *SecretInput, isCreate bool) error {
	in.Key = strings.TrimSpace(in.Key)
	if isCreate || in.Key != "" {
		if len(in.Key) == 0 || len(in.Key) > maxKeyLength || !keyPattern.MatchString(in.Key) {
			return fmt.Errorf("%w: key must start with a letter or underscore and contain only letters, digits, '_', '.', or '-' (max %d)", ErrValidation, maxKeyLength)
		}
	}
	if in.Kind == "" {
		in.Kind = "secret"
	}
	switch in.Kind {
	case "secret", "variable", "file":
	default:
		return fmt.Errorf("%w: kind must be secret, variable, or file", ErrValidation)
	}
	limit := maxValueBytes
	if in.Kind == "file" {
		limit = maxFileBytes
	}
	if len(in.Value) > limit {
		return fmt.Errorf("%w: value exceeds %d bytes", ErrValidation, limit)
	}
	if len(in.Comment) > maxCommentLength {
		return fmt.Errorf("%w: comment exceeds %d bytes", ErrValidation, maxCommentLength)
	}
	if len(in.ChangeMessage) > maxMessageLength {
		return fmt.Errorf("%w: change message exceeds %d bytes", ErrValidation, maxMessageLength)
	}
	if len(in.Notes) > 16*1024 {
		return fmt.Errorf("%w: notes exceed 16 KiB", ErrValidation)
	}
	if len(in.ProviderURL) > 2048 {
		return fmt.Errorf("%w: provider URL exceeds 2048 bytes", ErrValidation)
	}
	if in.ProviderURL != "" {
		u, err := url.Parse(in.ProviderURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: provider URL must be an http(s) URL", ErrValidation)
		}
	}
	if len(in.Folder) > 512 {
		return fmt.Errorf("%w: folder path exceeds 512 bytes", ErrValidation)
	}
	if len(in.Tags) > 20 {
		return fmt.Errorf("%w: at most 20 tags are allowed", ErrValidation)
	}
	for _, tag := range in.Tags {
		if len(tag) == 0 || len(tag) > 32 {
			return fmt.Errorf("%w: tags must be 1-32 characters", ErrValidation)
		}
	}
	return nil
}

func tagsJSON(tags []string) string {
	if len(tags) == 0 {
		return "[]"
	}
	raw, err := json.Marshal(tags)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func parseTags(raw string) []string {
	if raw == "" {
		return nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil
	}
	return tags
}

func (s *Service) resolveEnvironment(ctx context.Context, _ authz.Actor, orgSlug, projectSlug, envSlug string) (*store.Org, *store.Project, *store.Environment, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, err
	}
	project, err := s.Store.GetProjectBySlug(ctx, org.ID, projectSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, envSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, err
	}
	return org, project, env, nil
}

func secretScope(org *store.Org, project *store.Project, env *store.Environment) authz.Scope {
	return authz.Scope{OrgID: org.ID, ProjectID: project.ID, Environment: env.Slug}
}

// enforceCLIProduction blocks CLI-originated writes to production when the org
// policy requires it.
func (s *Service) enforceWritePolicy(ctx context.Context, org *store.Org, env *store.Environment, meta AuditMeta) error {
	if err := s.enforceCLIProduction(org, env, meta); err != nil {
		return err
	}
	freeze, err := s.Store.ActiveFreeze(ctx, env.ID, s.Now().UTC())
	if err == nil && freeze != nil {
		return fmt.Errorf("%w: %s until %s", ErrEnvironmentFrozen, freeze.Reason, freeze.EndsAt.Format(time.RFC3339))
	}
	return nil
}

func (s *Service) enforceCLIProduction(org *store.Org, env *store.Environment, meta AuditMeta) error {
	if env.Type != "production" {
		return nil
	}
	if !strings.HasPrefix(strings.ToLower(meta.UserAgent), "clavis-cli/") {
		return nil
	}
	if ParseOrgSettings(org.Settings).BlockCLIWrites {
		return ErrCLIBlocked
	}
	return nil
}

// authorizeSecretAccess applies env-type policy on top of RBAC: agents are
// denied production, and protected environments need an Admin-level role for
// writes.
func (s *Service) authorizeSecretAccess(actor authz.Actor, org *store.Org, project *store.Project, env *store.Environment, perm authz.Permission) error {
	if actor.Kind == authz.SubjectAgent && (env.Type == "production" || env.Type == "staging") {
		return ErrForbidden
	}
	scope := secretScope(org, project, env)
	if err := s.Authorize(actor, perm, scope); err != nil {
		return err
	}
	if env.IsProtected && (perm == authz.SecretsWrite || perm == authz.SecretsDelete) {
		if !s.Engine.Authorize(actor, authz.SettingsManage, scope, s.Now()).Allowed {
			return ErrProtected
		}
	}
	return nil
}

// ListSecrets returns live items in an environment. reveal requires
// secrets:read-values and is audited once for the batch.
func (s *Service) ListSecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, reveal bool, channel string, meta AuditMeta) ([]Secret, string, error) {
	if channel == "" {
		channel = "ui"
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, "", err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, "", err
	}
	if reveal {
		if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
			return nil, "", err
		}
	}

	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, "", err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	groups, err := s.Store.ListGroups(ctx, org.ID)
	if err != nil {
		return nil, "", err
	}
	groupNames := make(map[string]string, len(groups))
	for _, group := range groups {
		groupNames[group.ID] = group.Name
	}

	chain, err := s.environmentChain(ctx, env)
	if err != nil {
		return nil, "", err
	}
	overrides, err := s.loadOverrides(ctx, valueKey, org, project, env, actor.ID)
	if err != nil {
		return nil, "", err
	}
	sharedSets, err := s.sharedSetValues(ctx, org, env)
	if err != nil {
		return nil, "", err
	}
	seen := make(map[string]bool)
	var allItems []store.Item
	var resolvedItems []struct {
		env  *store.Environment
		item *store.Item
	}
	out := make([]Secret, 0, 16)
	for _, sourceEnv := range chain {
		items, err := s.Store.ListItems(ctx, sourceEnv.ID)
		if err != nil {
			return nil, "", err
		}
		allItems = append(allItems, items...)
		folders, err := s.Store.ListFolders(ctx, sourceEnv.ID)
		if err != nil {
			return nil, "", err
		}
		folderPaths := make(map[string]string, len(folders))
		for _, folder := range folders {
			folderPaths[folder.ID] = folder.Path
		}
		for i := range items {
			item := &items[i]
			key, err := s.decryptKey(valueKey, org, project, sourceEnv, item)
			if err != nil {
				return nil, "", err
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			folder := ""
			if item.FolderID != nil {
				folder = folderPaths[*item.FolderID]
			}
			view := Secret{
				ItemID: item.ID, Key: key, Kind: item.Kind, Folder: folder, Tags: parseTags(item.Tags),
				Version: item.Version, UpdatedAt: item.UpdatedAt, ExpiresAt: item.ExpiresAt,
				SourceEnvironment: sourceEnv.Slug, Inherited: sourceEnv.ID != env.ID,
			}
			if item.OwnerGroupID != nil {
				view.OwnerGroupID = *item.OwnerGroupID
				view.OwnerGroupName = groupNames[*item.OwnerGroupID]
			}
			if len(item.CommentCiphertext) > 0 {
				comment, err := decryptField(valueKey, org, project, sourceEnv, item, "comment", item.Version, item.CommentCiphertext)
				if err != nil {
					return nil, "", err
				}
				view.Comment = string(comment)
			}
			if len(item.NotesCiphertext) > 0 {
				notes, _, err := valueKey.Decrypt(item.NotesCiphertext,
					crypto.BindAAD(org.ID, project.ID, sourceEnv.ID, item.ID, "notes", 1))
				if err == nil {
					view.Notes = string(notes)
				}
			}
			view.ProviderURL = item.ProviderURL
			if item.Kind == "variable" && len(item.ValueCiphertext) > 0 {
				value, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
				if err != nil {
					return nil, "", err
				}
				view.Value = string(value)
				view.Revealed = true
			} else if reveal && len(item.ValueCiphertext) > 0 {
				value, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
				if err != nil {
					return nil, "", err
				}
				view.Value = string(value)
				view.Revealed = true
			}
			if override, ok := overrides[key]; ok {
				view.Overridden = true
				if view.Revealed {
					value, err := overrideValue(valueKey, org, project, env, override)
					if err != nil {
						return nil, "", err
					}
					view.Value = value
				}
			}
			out = append(out, view)
			resolvedItems = append(resolvedItems, struct {
				env  *store.Environment
				item *store.Item
			}{env: sourceEnv, item: item})
		}
	}
	for _, key := range sortedSharedKeys(sharedSets) {
		if seen[key] {
			continue
		}
		value := sharedSets[key]
		view := Secret{
			Key: key, Kind: value.Kind, SourceEnvironment: value.SetSlug,
			Inherited: true,
		}
		if value.Kind == "variable" || reveal {
			view.Value = value.Value
			view.Revealed = true
		}
		out = append(out, view)
	}
	applySharedSetReferences(out, sharedSets)
	if reveal {
		for _, entry := range resolvedItems {
			if entry.item.Kind == "variable" {
				continue
			}
			if err := s.Store.InsertRevealEvent(ctx, uuid.NewString(), actor.ID, entry.item.ID, channel, entry.item.Version); err != nil {
				s.Logger.Error("record reveal event failed", "item_id", entry.item.ID, "error", err)
			}
		}
		if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.list_reveal",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug, Count: len(out), Channel: channel}, meta); err != nil {
			s.Logger.Error("audit secret.list_reveal failed", "env_id", env.ID, "error", err)
		}
	}
	return out, etagForItems(allItems), nil
}

func etagForItems(items []store.Item) string {
	h := sha256.New()
	for _, item := range items {
		_, _ = fmt.Fprintf(h, "%s:%d:%d;", item.ID, item.Version, item.UpdatedAt.UnixNano())
	}
	return `"` + hex.EncodeToString(h.Sum(nil)) + `"`
}

// GetSecret loads one item by key, resolving inherited environments.
func (s *Service) GetSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, reveal bool, reason, channel string, meta AuditMeta) (*Secret, error) {
	if channel == "" {
		channel = "ui"
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadNames); err != nil {
		return nil, err
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	blind := crypto.BlindIndex(indexKey, key)
	sharedSets, err := s.sharedSetValues(ctx, org, env)
	if err != nil {
		return nil, err
	}
	chain, err := s.environmentChain(ctx, env)
	if err != nil {
		return nil, err
	}
	var item *store.Item
	var sourceEnv *store.Environment
	for _, candidate := range chain {
		found, err := s.Store.GetItemByBlindIndex(ctx, candidate.ID, blind)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		item, sourceEnv = found, candidate
		break
	}
	if item == nil {
		value, ok := sharedSets[key]
		if !ok {
			return nil, ErrNotFound
		}
		view := &Secret{
			Key: key, Kind: value.Kind, SourceEnvironment: value.SetSlug,
			Inherited: true,
		}
		if value.Kind == "variable" || reveal {
			if reveal && env.Type == "production" && strings.TrimSpace(reason) == "" {
				return nil, fmt.Errorf("%w: a reason is required to reveal values in production", ErrValidation)
			}
			if reveal {
				if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
					return nil, err
				}
			}
			view.Value = expandSetValue(value.Value, sharedSets)
			view.Revealed = true
		}
		return view, nil
	}

	folder := ""
	if item.FolderID != nil {
		if f, err := s.Store.GetFolderByID(ctx, *item.FolderID); err == nil {
			folder = f.Path
		}
	}
	view := &Secret{
		ItemID: item.ID, Key: key, Kind: item.Kind, Folder: folder, Tags: parseTags(item.Tags),
		Version: item.Version, UpdatedAt: item.UpdatedAt, ExpiresAt: item.ExpiresAt,
		SourceEnvironment: sourceEnv.Slug, Inherited: sourceEnv.ID != env.ID,
	}
	if item.OwnerGroupID != nil {
		view.OwnerGroupID = *item.OwnerGroupID
		if group, err := s.Store.GetGroup(ctx, org.ID, *item.OwnerGroupID); err == nil {
			view.OwnerGroupName = group.Name
		}
	}
	if len(item.CommentCiphertext) > 0 {
		comment, err := decryptField(valueKey, org, project, sourceEnv, item, "comment", item.Version, item.CommentCiphertext)
		if err != nil {
			return nil, err
		}
		view.Comment = string(comment)
	}
	if len(item.NotesCiphertext) > 0 {
		notes, _, err := valueKey.Decrypt(item.NotesCiphertext,
			crypto.BindAAD(org.ID, project.ID, sourceEnv.ID, item.ID, "notes", 1))
		if err == nil {
			view.Notes = string(notes)
		}
	}
	view.ProviderURL = item.ProviderURL
	if item.Kind == "variable" {
		if len(item.ValueCiphertext) > 0 {
			value, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
			if err != nil {
				return nil, err
			}
			view.Value = expandSetValue(string(value), sharedSets)
			view.Revealed = true
		}
		return view, nil
	}
	if reveal && env.Type == "production" && strings.TrimSpace(reason) == "" {
		return nil, fmt.Errorf("%w: a reason is required to reveal values in production", ErrValidation)
	}
	if reveal {
		if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
			return nil, err
		}
		value, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
		if err != nil {
			return nil, err
		}
		view.Value = expandSetValue(string(value), sharedSets)
		view.Revealed = true
		if err := s.Store.InsertRevealEvent(ctx, uuid.NewString(), actor.ID, item.ID, channel, item.Version); err != nil {
			s.Logger.Error("record reveal event failed", "item_id", item.ID, "error", err)
		}
		if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.reveal",
			audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: sourceEnv.ID},
			audit.Details{KeyBlindIndex: blind, Environment: sourceEnv.Slug, Version: item.Version, Reason: reason, Channel: channel}, meta); err != nil {
			s.Logger.Error("audit secret.reveal failed", "item_id", item.ID, "error", err)
		}
	}
	if override, ok, err := s.overrideForKey(ctx, env, actor.ID, blind); err != nil {
		return nil, err
	} else if ok {
		view.Overridden = true
		if view.Revealed {
			value, err := overrideValue(valueKey, org, project, env, override)
			if err != nil {
				return nil, err
			}
			view.Value = value
		}
	}
	return view, nil
}

// CreateSecret encrypts and stores a new item.
func (s *Service) CreateSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string, in SecretInput, meta AuditMeta) (*Secret, error) {
	if err := validateSecretInput(&in, true); err != nil {
		return nil, err
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return nil, err
	}
	if err := s.validateAgainstSchema(ctx, project, &in); err != nil {
		return nil, err
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	folderID, err := s.ensureFolder(ctx, env.ID, in.Folder)
	if err != nil {
		return nil, err
	}
	itemID := uuid.NewString()
	blind := crypto.BlindIndex(indexKey, in.Key)
	keyCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Key),
		crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "key", 1))
	if err != nil {
		return nil, err
	}
	valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Value),
		crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "value", 1))
	if err != nil {
		return nil, err
	}
	var commentCT []byte
	if in.Comment != "" {
		commentCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Comment),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "comment", 1))
		if err != nil {
			return nil, err
		}
	}

	ownerGroupID, ownerGroupName, err := s.resolveOwnerGroup(ctx, org.ID, in.OwnerGroupID)
	if err != nil {
		return nil, err
	}
	var notesCT []byte
	if in.Notes != "" {
		notesCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Notes),
			crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "notes", 1))
		if err != nil {
			return nil, err
		}
	}
	item := &store.Item{
		ID:                itemID,
		EnvironmentID:     env.ID,
		FolderID:          folderID,
		Kind:              in.Kind,
		KeyCiphertext:     keyCT,
		KeyBlindIndex:     blind,
		ValueCiphertext:   valueCT,
		CommentCiphertext: commentCT,
		NotesCiphertext:   notesCT,
		ProviderURL:       in.ProviderURL,
		OwnerGroupID:      ownerGroupID,
		Tags:              tagsJSON(in.Tags),
		ExpiresAt:         in.ExpiresAt,
		Version:           1,
	}
	message := in.ChangeMessage
	if message == "" {
		message = "Created"
	}
	if err := s.Store.CreateItem(ctx, item, message); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.create",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug, Version: 1, Reason: in.ChangeMessage}, meta); err != nil {
		s.Logger.Error("audit secret.create failed", "item_id", item.ID, "error", err)
	}
	return &Secret{Key: in.Key, Kind: in.Kind, Comment: in.Comment, Folder: in.Folder,
		Notes: in.Notes, ProviderURL: in.ProviderURL, OwnerGroupID: in.OwnerGroupID,
		OwnerGroupName: ownerGroupName, Tags: in.Tags, Version: 1, UpdatedAt: item.UpdatedAt}, nil
}

// UpdateSecret creates a new version of an existing item.
func (s *Service) UpdateSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, in SecretInput, meta AuditMeta) (*Secret, error) {
	in.Key = key
	if err := validateSecretInput(&in, false); err != nil {
		return nil, err
	}
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return nil, err
	}
	if !in.KeepValue {
		if err := s.validateAgainstSchema(ctx, project, &in); err != nil {
			return nil, err
		}
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetItemByBlindIndex(ctx, env.ID, blind)
	if err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		// Editing an inherited key creates a local override instead of
		// mutating the parent environment.
		chain, chainErr := s.environmentChain(ctx, env)
		if chainErr != nil {
			return nil, chainErr
		}
		inherited := false
		for _, candidate := range chain[1:] {
			if _, lookupErr := s.Store.GetItemByBlindIndex(ctx, candidate.ID, blind); lookupErr == nil {
				inherited = true
				break
			}
		}
		if !inherited {
			if shared, lookupErr := s.sharedSetValues(ctx, org, env); lookupErr == nil {
				_, inherited = shared[key]
			}
		}
		if inherited {
			if in.KeepValue {
				return nil, fmt.Errorf("%w: enter a value to override an inherited key", ErrValidation)
			}
			return s.CreateSecret(ctx, actor, orgSlug, projectSlug, envSlug, in, meta)
		}
		return nil, ErrNotFound
	}
	folderID, err := s.ensureFolder(ctx, env.ID, in.Folder)
	if err != nil {
		return nil, err
	}
	next := item.Version + 1
	plainValue := crypto.Secret(in.Value)
	if in.KeepValue {
		plainValue, err = decryptField(valueKey, org, project, env, item, "value", item.Version, item.ValueCiphertext)
		if err != nil {
			return nil, err
		}
		defer plainValue.Zero()
	}
	valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), plainValue,
		crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", next))
	if err != nil {
		return nil, err
	}
	var commentCT []byte
	if in.Comment != "" {
		commentCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Comment),
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "comment", next))
		if err != nil {
			return nil, err
		}
	}
	ownerGroupID, ownerGroupName, err := s.resolveOwnerGroup(ctx, org.ID, in.OwnerGroupID)
	if err != nil {
		return nil, err
	}
	var notesCT []byte
	if in.Notes != "" {
		notesCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(in.Notes),
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "notes", 1))
		if err != nil {
			return nil, err
		}
	}
	item.Kind = in.Kind
	item.FolderID = folderID
	item.NotesCiphertext = notesCT
	item.ProviderURL = in.ProviderURL
	item.OwnerGroupID = ownerGroupID
	item.ExpiresAt = in.ExpiresAt
	item.ValueCiphertext = valueCT
	item.CommentCiphertext = commentCT
	item.Tags = tagsJSON(in.Tags)
	if err := s.Store.UpdateItem(ctx, item, in.ChangeMessage, actor.ID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.update",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug, Version: item.Version, Reason: in.ChangeMessage}, meta); err != nil {
		s.Logger.Error("audit secret.update failed", "item_id", item.ID, "error", err)
	}
	return &Secret{Key: key, Kind: in.Kind, Comment: in.Comment, Folder: in.Folder,
		Notes: in.Notes, ProviderURL: in.ProviderURL, OwnerGroupID: in.OwnerGroupID,
		OwnerGroupName: ownerGroupName, Tags: in.Tags, Version: item.Version, UpdatedAt: item.UpdatedAt}, nil
}

// DeleteSecret soft-deletes an item.
func (s *Service) DeleteSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, meta AuditMeta) error {
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
	_, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return err
	}
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetItemByBlindIndex(ctx, env.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.SoftDeleteItem(ctx, item.ID); err != nil {
		return err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.delete",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug, Version: item.Version}, meta); err != nil {
		s.Logger.Error("audit secret.delete failed", "item_id", item.ID, "error", err)
	}
	return nil
}

// ListSecretVersions returns history metadata without values.
func (s *Service) ListSecretVersions(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string) ([]SecretVersion, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SecretsReadNames, secretScope(org, project, env)); err != nil {
		return nil, err
	}
	_, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer indexKey.Zero()
	item, err := s.Store.GetItemByBlindIndex(ctx, env.ID, crypto.BlindIndex(indexKey, key))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	versions, err := s.Store.ListItemVersions(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SecretVersion, 0, len(versions))
	for _, v := range versions {
		out = append(out, SecretVersion{
			Version:       v.Version,
			ChangeMessage: v.ChangeMessage,
			ActorID:       v.ActorID,
			CreatedAt:     v.CreatedAt,
		})
	}
	return out, nil
}

// RollbackSecret copies an old version into a new version.
func (s *Service) RollbackSecret(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, key string, version int, meta AuditMeta) (*Secret, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
		return nil, err
	}
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	blind := crypto.BlindIndex(indexKey, key)
	item, err := s.Store.GetItemByBlindIndex(ctx, env.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	old, err := s.Store.GetItemVersion(ctx, item.ID, version)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	plaintext, _, err := valueKey.Decrypt(old.ValueCiphertext,
		crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", version))
	if err != nil {
		return nil, fmt.Errorf("service: decrypt old version: %w", err)
	}
	defer plaintext.Zero()
	next := item.Version + 1
	valueCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), plaintext,
		crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "value", next))
	if err != nil {
		return nil, err
	}
	item.ValueCiphertext = valueCT
	var commentCT []byte
	if len(item.CommentCiphertext) > 0 {
		plain, _, err := valueKey.Decrypt(item.CommentCiphertext,
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "comment", item.Version))
		if err != nil {
			return nil, fmt.Errorf("service: decrypt old comment: %w", err)
		}
		defer plain.Zero()
		commentCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), plain,
			crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, "comment", next))
		if err != nil {
			return nil, err
		}
	}
	item.CommentCiphertext = commentCT
	message := fmt.Sprintf("Rolled back to version %d", version)
	if err := s.Store.UpdateItem(ctx, item, message, actor.ID); err != nil {
		return nil, err
	}
	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secret.rollback",
		audit.Resource{Type: "secret", ID: item.ID, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{KeyBlindIndex: blind, Environment: env.Slug, Version: item.Version, Reason: message}, meta); err != nil {
		s.Logger.Error("audit secret.rollback failed", "item_id", item.ID, "error", err)
	}
	return &Secret{Key: key, Kind: item.Kind, Version: item.Version, UpdatedAt: item.UpdatedAt}, nil
}

func (s *Service) decryptKey(valueKey crypto.Key, org *store.Org, project *store.Project, env *store.Environment, item *store.Item) (string, error) {
	key, err := decryptField(valueKey, org, project, env, item, "key", 1, item.KeyCiphertext)
	if err != nil {
		return "", err
	}
	return string(key), nil
}

func decryptField(valueKey crypto.Key, org *store.Org, project *store.Project, env *store.Environment, item *store.Item, field string, version int, ciphertext []byte) (crypto.Secret, error) {
	plaintext, _, err := valueKey.Decrypt(ciphertext, crypto.BindAAD(org.ID, project.ID, env.ID, item.ID, field, version))
	if err != nil {
		return nil, fmt.Errorf("service: decrypt %s: %w", field, err)
	}
	return plaintext, nil
}

func (s *Service) ensureFolder(ctx context.Context, envID, rawPath string) (*string, error) {
	path := strings.TrimSpace(rawPath)
	if path == "" || path == "/" {
		return nil, nil
	}
	normalized := authz.NormalizePath(path)
	if normalized == "/" {
		return nil, nil
	}
	folder, err := s.Store.GetFolderByPath(ctx, envID, normalized)
	if err == nil {
		return &folder.ID, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	folder = &store.Folder{ID: uuid.NewString(), EnvironmentID: envID, Path: normalized}
	if err := s.Store.CreateFolder(ctx, folder); err != nil {
		if errors.Is(err, store.ErrConflict) {
			if existing, lookupErr := s.Store.GetFolderByPath(ctx, envID, normalized); lookupErr == nil {
				return &existing.ID, nil
			}
		}
		return nil, err
	}
	return &folder.ID, nil
}

// resolveOwnerGroup validates an optional owner group ID.
func (s *Service) resolveOwnerGroup(ctx context.Context, orgID, groupID string) (*string, string, error) {
	groupID = strings.TrimSpace(groupID)
	if groupID == "" {
		return nil, "", nil
	}
	group, err := s.Store.GetGroup(ctx, orgID, groupID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, "", fmt.Errorf("%w: owner group does not exist", ErrValidation)
		}
		return nil, "", err
	}
	return &group.ID, group.Name, nil
}

// environmentChain returns the environment followed by its inheritance parents,
// rejecting cycles.
func (s *Service) environmentChain(ctx context.Context, env *store.Environment) ([]*store.Environment, error) {
	chain := []*store.Environment{env}
	seen := map[string]bool{env.ID: true}
	current := env
	for current.ParentID != nil && *current.ParentID != "" {
		if seen[*current.ParentID] {
			return nil, fmt.Errorf("%w: environment inheritance cycle", ErrValidation)
		}
		parent, err := s.Store.GetEnvironmentByID(ctx, *current.ParentID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				break
			}
			return nil, err
		}
		chain = append(chain, parent)
		seen[parent.ID] = true
		current = parent
	}
	return chain, nil
}

// overrideForKey loads one personal override by blind index.
func (s *Service) overrideForKey(ctx context.Context, env *store.Environment, userID, blind string) (*store.Item, bool, error) {
	overlay, err := s.Store.GetOverlay(ctx, env.ID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	item, err := s.Store.GetOverlayItemByBlindIndex(ctx, overlay.ID, blind)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return item, true, nil
}
