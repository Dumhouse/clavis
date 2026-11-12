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

// ErrPrecondition is returned when an If-Match ETag is stale.
var ErrPrecondition = errors.New("service: precondition failed")

// BatchInput is an atomic set of changes and deletes for one environment.
type BatchInput struct {
	Changes []SecretInput
	Deletes []string
	Message string
}

// BatchResult reports what an atomic batch changed.
type BatchResult struct {
	ETag    string
	Created int
	Updated int
	Deleted int
}

// BatchSecrets applies creates, updates, and deletes atomically. ifMatch must
// equal the environment ETag returned by ListSecrets.
func (s *Service) BatchSecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, ifMatch string, in BatchInput, meta AuditMeta) (*BatchResult, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return nil, err
	}
	if err := s.enforceCLIProduction(org, env, meta); err != nil {
		return nil, err
	}
	items, err := s.Store.ListItems(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	currentETag := etagForItems(items)
	if normalizeETag(ifMatch) == "" || normalizeETag(ifMatch) != normalizeETag(currentETag) {
		return nil, ErrPrecondition
	}

	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	byBlind := make(map[string]*store.Item, len(items))
	for i := range items {
		byBlind[items[i].KeyBlindIndex] = &items[i]
	}

	message := in.Message
	if message == "" {
		message = "Batch update"
	}
	var (
		creates []store.ItemWrite
		updates []store.ItemWrite
		deletes []store.ItemWrite
		changed []string
	)
	seen := make(map[string]bool)
	for _, change := range in.Changes {
		change.Key = strings.TrimSpace(change.Key)
		if seen[change.Key] {
			return nil, fmt.Errorf("%w: duplicate key %s in batch", ErrValidation, change.Key)
		}
		seen[change.Key] = true
		if err := validateSecretInput(&change, true); err != nil {
			return nil, err
		}
		ownerGroupID, _, err := s.resolveOwnerGroup(ctx, org.ID, change.OwnerGroupID)
		if err != nil {
			return nil, err
		}
		blind := crypto.BlindIndex(indexKey, change.Key)
		item, exists := byBlind[blind]
		if !exists {
			itemID := uuid.NewString()
			keyCT, err := valueKey.Encrypt(uint32(project.KeyVersion), crypto.Secret(change.Key),
				crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "key", 1))
			if err != nil {
				return nil, err
			}
			valueCT, commentCT, err := s.sealBatchFields(valueKey, org, project, env, itemID, 1, change.Value, change.Comment)
			if err != nil {
				return nil, err
			}
			item = &store.Item{
				ID:                itemID,
				EnvironmentID:     env.ID,
				Kind:              change.Kind,
				KeyCiphertext:     keyCT,
				KeyBlindIndex:     blind,
				ValueCiphertext:   valueCT,
				CommentCiphertext: commentCT,
				Tags:              tagsJSON(change.Tags),
				OwnerGroupID:      ownerGroupID,
				Version:           1,
			}
			creates = append(creates, store.ItemWrite{Item: item, ChangeMessage: message, ActorID: actor.ID})
			changed = append(changed, blind)
			continue
		}
		next := item.Version + 1
		valueCT, commentCT, err := s.sealBatchFields(valueKey, org, project, env, item.ID, next, change.Value, change.Comment)
		if err != nil {
			return nil, err
		}
		item.Kind = change.Kind
		item.ValueCiphertext = valueCT
		item.CommentCiphertext = commentCT
		item.Tags = tagsJSON(change.Tags)
		item.OwnerGroupID = ownerGroupID
		item.Version = next
		updates = append(updates, store.ItemWrite{
			Item: item, PreviousVersion: next - 1, ChangeMessage: message, ActorID: actor.ID,
		})
		changed = append(changed, blind)
	}

	for _, key := range in.Deletes {
		key = strings.TrimSpace(key)
		if seen[key] {
			return nil, fmt.Errorf("%w: key %s is both changed and deleted", ErrValidation, key)
		}
		seen[key] = true
		blind := crypto.BlindIndex(indexKey, key)
		item, ok := byBlind[blind]
		if !ok {
			return nil, fmt.Errorf("%w: secret %s does not exist", ErrNotFound, key)
		}
		deletes = append(deletes, store.ItemWrite{
			Item: item, PreviousVersion: item.Version, ChangeMessage: message, ActorID: actor.ID,
		})
		changed = append(changed, blind)
	}

	if len(changed) == 0 {
		return &BatchResult{ETag: currentETag}, nil
	}
	if len(changed) > 1000 {
		return nil, fmt.Errorf("%w: at most 1000 changes per batch", ErrValidation)
	}
	if err := s.Store.ApplyItemBatch(ctx, creates, updates, deletes); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrPrecondition
		}
		return nil, err
	}

	s.publish(org.ID, "secrets.changed", project, env)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secrets.batch",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{
			Environment: env.Slug, Count: len(changed), ChangedKeys: changed,
			Reason: message,
		}, meta); err != nil {
		s.Logger.Error("audit secrets.batch failed", "env_id", env.ID, "error", err)
	}

	after, err := s.Store.ListItems(ctx, env.ID)
	if err != nil {
		return nil, err
	}
	return &BatchResult{
		ETag: etagForItems(after), Created: len(creates), Updated: len(updates), Deleted: len(deletes),
	}, nil
}

func (s *Service) sealBatchFields(valueKey crypto.Key, org *store.Org, project *store.Project, env *store.Environment, itemID string, version int, value, comment string) ([]byte, []byte, error) {
	valueCT, err := valueKey.Encrypt(uint32(project.KeyVersion), crypto.Secret(value),
		crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "value", version))
	if err != nil {
		return nil, nil, err
	}
	if comment == "" {
		return valueCT, nil, nil
	}
	commentCT, err := valueKey.Encrypt(uint32(project.KeyVersion), crypto.Secret(comment),
		crypto.BindAAD(org.ID, project.ID, env.ID, itemID, "comment", version))
	if err != nil {
		return nil, nil, err
	}
	return valueCT, commentCT, nil
}

func normalizeETag(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"`)
}
