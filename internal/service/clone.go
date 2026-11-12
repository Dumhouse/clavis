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

// CloneEnvironmentInput describes a cloned environment.
type CloneEnvironmentInput struct {
	Slug          string
	Name          string
	IncludeValues bool
}

// CloneEnvironment copies an environment's keys (and optionally values) into a
// new sibling environment in the same project.
func (s *Service) CloneEnvironment(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, sourceSlug string, in CloneEnvironmentInput, meta AuditMeta) (*store.Environment, error) {
	org, project, source, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, sourceSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.EnvironmentsManage, secretScope(org, project, source)); err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SecretsReadNames, secretScope(org, project, source)); err != nil {
		return nil, err
	}
	if in.IncludeValues {
		if err := s.Authorize(actor, authz.SecretsReadValues, secretScope(org, project, source)); err != nil {
			return nil, err
		}
	}
	slug, err := NormalizeSlug(in.Slug)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: environment name is required", ErrValidation)
	}
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return nil, err
	}

	target := &store.Environment{
		ID:          uuid.NewString(),
		ProjectID:   project.ID,
		Slug:        slug,
		Name:        name,
		Type:        source.Type,
		Color:       source.Color,
		Position:    len(envs),
		IsProtected: source.IsProtected,
	}
	if err := s.Store.CreateEnvironment(ctx, target); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}

	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()

	items, err := s.Store.ListItems(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	creates := make([]store.ItemWrite, 0, len(items))
	for i := range items {
		src := &items[i]
		key, err := s.decryptKey(valueKey, org, project, source, src)
		if err != nil {
			return nil, err
		}
		itemID := uuid.NewString()
		keyCT, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(key),
			crypto.BindAAD(org.ID, project.ID, target.ID, itemID, "key", 1))
		if err != nil {
			return nil, err
		}
		var valueCT []byte
		if in.IncludeValues && len(src.ValueCiphertext) > 0 {
			value, err := decryptField(valueKey, org, project, source, src, "value", src.Version, src.ValueCiphertext)
			if err != nil {
				return nil, err
			}
			valueCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), value,
				crypto.BindAAD(org.ID, project.ID, target.ID, itemID, "value", 1))
			value.Zero()
			if err != nil {
				return nil, err
			}
		}
		var commentCT []byte
		if len(src.CommentCiphertext) > 0 {
			comment, err := decryptField(valueKey, org, project, source, src, "comment", src.Version, src.CommentCiphertext)
			if err != nil {
				return nil, err
			}
			commentCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), comment,
				crypto.BindAAD(org.ID, project.ID, target.ID, itemID, "comment", 1))
			comment.Zero()
			if err != nil {
				return nil, err
			}
		}
		folderID := (*string)(nil)
		if src.FolderID != nil {
			folder, err := s.Store.GetFolderByID(ctx, *src.FolderID)
			if err != nil {
				return nil, err
			}
			folderID, err = s.ensureFolder(ctx, target.ID, folder.Path)
			if err != nil {
				return nil, err
			}
		}
		var notesCT []byte
		if len(src.NotesCiphertext) > 0 {
			notes, err := decryptField(valueKey, org, project, source, src, "notes", src.Version, src.NotesCiphertext)
			if err != nil {
				return nil, err
			}
			notesCT, err = valueKey.Encrypt(keyVersion(project.KeyVersion), notes,
				crypto.BindAAD(org.ID, project.ID, target.ID, itemID, "notes", 1))
			notes.Zero()
			if err != nil {
				return nil, err
			}
		}
		creates = append(creates, store.ItemWrite{Item: &store.Item{
			ID: itemID, EnvironmentID: target.ID, FolderID: folderID, Kind: src.Kind,
			KeyCiphertext: keyCT, KeyBlindIndex: crypto.BlindIndex(indexKey, key),
			ValueCiphertext: valueCT, CommentCiphertext: commentCT,
			Tags: src.Tags, OwnerGroupID: src.OwnerGroupID, NotesCiphertext: notesCT,
			ProviderURL: src.ProviderURL, ExpiresAt: src.ExpiresAt, Version: 1,
		}, ChangeMessage: "Cloned from " + source.Slug, ActorID: actor.ID})
	}
	if len(creates) > 0 {
		if err := s.Store.ApplyItemBatch(ctx, creates, nil, nil); err != nil {
			return nil, err
		}
	}
	s.publish(org.ID, "environments.changed", project, target)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.clone",
		audit.Resource{Type: "environment", ID: target.ID, Name: target.Slug, ProjectID: project.ID, EnvID: target.ID},
		audit.Details{Environment: target.Slug, Version: len(creates), Reason: source.Slug}, meta); err != nil {
		s.Logger.Error("audit env.clone failed", "env_id", target.ID, "error", err)
	}
	return target, nil
}
