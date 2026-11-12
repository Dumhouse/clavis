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

// EnvSecretChange is one change addressed to an environment slug.
type EnvSecretChange struct {
	Environment string
	Input       SecretInput
}

// EnvSecretDelete is one delete addressed to an environment slug.
type EnvSecretDelete struct {
	Environment string
	Key         string
}

// ProjectBatchResult reports a project-wide atomic batch.
type ProjectBatchResult struct {
	Created      int
	Updated      int
	Deleted      int
	Environments map[string]BatchResult
}

// BatchProjectSecrets applies changes across several environments of one
// project in a single database transaction. ifMatch maps env slug to the ETag
// returned by ListSecrets; every touched environment must have a fresh ETag.
func (s *Service) BatchProjectSecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, ifMatch map[string]string, changes []EnvSecretChange, deletes []EnvSecretDelete, message string, meta AuditMeta) (*ProjectBatchResult, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SecretsWrite)
	if err != nil {
		return nil, err
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	envBySlug := make(map[string]*store.Environment, len(envs))
	for i := range envs {
		envBySlug[envs[i].Slug] = &envs[i]
	}

	touched := make(map[string]bool)
	for _, change := range changes {
		touched[change.Environment] = true
	}
	for _, del := range deletes {
		touched[del.Environment] = true
	}
	itemsByEnv := make(map[string][]store.Item, len(touched))
	for slug := range touched {
		env, ok := envBySlug[slug]
		if !ok {
			return nil, fmt.Errorf("%w: unknown environment %s", ErrValidation, slug)
		}
		if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
			return nil, err
		}
		if err := s.enforceCLIProduction(org, env, meta); err != nil {
			return nil, err
		}
		expected := normalizeETag(ifMatch[slug])
		if expected == "" {
			return nil, ErrPrecondition
		}
		items, err := s.Store.ListItems(ctx, env.ID)
		if err != nil {
			return nil, err
		}
		if expected != normalizeETag(etagForItems(items)) {
			return nil, ErrPrecondition
		}
		itemsByEnv[slug] = items
	}

	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	if message == "" {
		message = "Batch update"
	}
	var (
		creates []store.ItemWrite
		updates []store.ItemWrite
		remove  []store.ItemWrite
		result  = &ProjectBatchResult{Environments: make(map[string]BatchResult)}
	)
	seen := make(map[string]bool)
	for _, change := range changes {
		env := envBySlug[change.Environment]
		if env == nil {
			return nil, fmt.Errorf("%w: unknown environment %s", ErrValidation, change.Environment)
		}
		change.Input.Key = strings.TrimSpace(change.Input.Key)
		id := change.Environment + ":" + change.Input.Key
		if seen[id] {
			return nil, fmt.Errorf("%w: duplicate key %s in %s", ErrValidation, change.Input.Key, change.Environment)
		}
		seen[id] = true
		if err := validateSecretInput(&change.Input, true); err != nil {
			return nil, err
		}
		blind := crypto.BlindIndex(indexKey, change.Input.Key)
		var existing *store.Item
		for i := range itemsByEnv[change.Environment] {
			if itemsByEnv[change.Environment][i].KeyBlindIndex == blind {
				existing = &itemsByEnv[change.Environment][i]
				break
			}
		}
		if existing == nil {
			itemID := uuid.NewString()
			keyCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(change.Input.Key),
				crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "key", 1))
			if err != nil {
				return nil, err
			}
			valueCT, commentCT, err := s.sealBatchFields(valueKey, org, project, env, itemID, 1, change.Input.Value, change.Input.Comment)
			if err != nil {
				return nil, err
			}
			creates = append(creates, store.ItemWrite{Item: &store.Item{
				ID: itemID, EnvironmentID: env.ID, Kind: change.Input.Kind,
				KeyCiphertext: keyCT, KeyBlindIndex: blind, ValueCiphertext: valueCT,
				CommentCiphertext: commentCT, Tags: tagsJSON(change.Input.Tags), Version: 1,
			}, ChangeMessage: message, ActorID: actor.ID})
			continue
		}
		next := existing.Version + 1
		valueCT, commentCT, err := s.sealBatchFields(valueKey, org, project, env, existing.ID, next, change.Input.Value, change.Input.Comment)
		if err != nil {
			return nil, err
		}
		existing.Kind = change.Input.Kind
		existing.ValueCiphertext = valueCT
		existing.CommentCiphertext = commentCT
		existing.Tags = tagsJSON(change.Input.Tags)
		existing.Version = next
		updates = append(updates, store.ItemWrite{Item: existing, PreviousVersion: next - 1, ChangeMessage: message, ActorID: actor.ID})
	}

	for _, del := range deletes {
		env := envBySlug[del.Environment]
		if env == nil {
			return nil, fmt.Errorf("%w: unknown environment %s", ErrValidation, del.Environment)
		}
		id := del.Environment + ":" + del.Key
		if seen[id] {
			return nil, fmt.Errorf("%w: key %s is both changed and deleted", ErrValidation, del.Key)
		}
		seen[id] = true
		blind := crypto.BlindIndex(indexKey, del.Key)
		var existing *store.Item
		for i := range itemsByEnv[del.Environment] {
			if itemsByEnv[del.Environment][i].KeyBlindIndex == blind {
				existing = &itemsByEnv[del.Environment][i]
				break
			}
		}
		if existing == nil {
			return nil, fmt.Errorf("%w: secret %s does not exist in %s", ErrNotFound, del.Key, del.Environment)
		}
		remove = append(remove, store.ItemWrite{Item: existing, PreviousVersion: existing.Version, ActorID: actor.ID})
	}

	if err := s.Store.ApplyItemBatch(ctx, creates, updates, remove); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrPrecondition
		}
		return nil, err
	}

	// Refresh ETags and audit per environment.
	for slug := range touched {
		env := envBySlug[slug]
		after, err := s.Store.ListItems(ctx, env.ID)
		if err != nil {
			return nil, err
		}
		result.Environments[slug] = BatchResult{ETag: etagForItems(after)}
		s.publish(org.ID, "secrets.changed", project, env)
		changedKeys := make([]string, 0)
		for _, item := range after {
			changedKeys = append(changedKeys, item.KeyBlindIndex)
		}
		if err := s.record(ctx, toAuditActor(actor), org.ID, "secrets.batch",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug, Count: len(after), ChangedKeys: changedKeys, Reason: message}, meta); err != nil {
			s.Logger.Error("audit secrets.batch failed", "env_id", env.ID, "error", err)
		}
	}
	result.Created = len(creates)
	result.Updated = len(updates)
	result.Deleted = len(remove)
	return result, nil
}
