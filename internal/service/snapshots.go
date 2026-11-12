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
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

type snapshotSecret struct {
	Key          string   `json:"key"`
	Kind         string   `json:"kind"`
	Value        string   `json:"value"`
	Comment      string   `json:"comment,omitempty"`
	Folder       string   `json:"folder,omitempty"`
	Notes        string   `json:"notes,omitempty"`
	ProviderURL  string   `json:"provider_url,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	OwnerGroupID string   `json:"owner_group_id,omitempty"`
}

// SnapshotView describes a stored snapshot without exposing its payload.
type SnapshotView struct {
	ID        string
	Reason    string
	CreatedAt time.Time
}

// CreateSnapshot captures the environment's direct secrets as an encrypted
// point-in-time record.
func (s *Service) CreateSnapshot(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, reason string, meta AuditMeta) (*SnapshotView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsManage); err != nil {
		return nil, err
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > 512 {
		return nil, fmt.Errorf("%w: snapshot reason exceeds 512 bytes", ErrValidation)
	}
	secrets, _, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, envSlug, true, "snapshot", meta)
	if err != nil {
		return nil, err
	}
	payloadSecrets := make([]snapshotSecret, 0, len(secrets))
	for _, secret := range secrets {
		if secret.Inherited {
			continue
		}
		payloadSecrets = append(payloadSecrets, snapshotSecret{
			Key: secret.Key, Kind: secret.Kind, Value: secret.Value, Comment: secret.Comment,
			Folder: secret.Folder, Notes: secret.Notes, ProviderURL: secret.ProviderURL,
			Tags: secret.Tags, OwnerGroupID: secret.OwnerGroupID,
		})
	}
	raw, err := json.Marshal(payloadSecrets)
	if err != nil {
		return nil, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	snapshot := &store.Snapshot{
		EnvID: env.ID, Reason: reason, CreatedAt: s.Now().UTC(),
	}
	// The ID is needed for AAD, so generate it before encrypting.
	snapshot.ID = uuid.NewString()
	ciphertext, err := valueKey.Encrypt(keyVersion(project.KeyVersion), crypto.Secret(raw),
		crypto.BindAAD(org.ID, project.ID, env.ID, snapshot.ID, "snapshot", 1))
	if err != nil {
		return nil, err
	}
	snapshot.Payload = ciphertext
	if err := s.Store.CreateSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.snapshot",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: reason, Count: len(payloadSecrets)}, meta); err != nil {
		s.Logger.Error("audit env.snapshot failed", "snapshot_id", snapshot.ID, "error", err)
	}
	return &SnapshotView{ID: snapshot.ID, Reason: snapshot.Reason, CreatedAt: snapshot.CreatedAt}, nil
}

// ListSnapshots returns recent snapshots for an environment.
func (s *Service) ListSnapshots(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug string) ([]SnapshotView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.EnvironmentsRead); err != nil {
		return nil, err
	}
	_ = project
	snapshots, err := s.Store.ListSnapshots(ctx, env.ID, 100)
	if err != nil {
		return nil, err
	}
	out := make([]SnapshotView, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, SnapshotView{ID: snapshot.ID, Reason: snapshot.Reason, CreatedAt: snapshot.CreatedAt})
	}
	return out, nil
}

// RestoreSnapshot writes every captured secret as a new version.
func (s *Service) RestoreSnapshot(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, snapshotID string, meta AuditMeta) (int, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return 0, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsWrite); err != nil {
		return 0, err
	}
	if err := s.enforceWritePolicy(ctx, org, env, meta); err != nil {
		return 0, err
	}
	snapshot, err := s.Store.GetSnapshot(ctx, env.ID, snapshotID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, ErrNotFound
		}
		return 0, err
	}
	valueKey, _, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return 0, err
	}
	defer valueKey.Zero()
	raw, _, err := valueKey.Decrypt(snapshot.Payload,
		crypto.BindAAD(org.ID, project.ID, env.ID, snapshot.ID, "snapshot", 1))
	if err != nil {
		return 0, fmt.Errorf("%w: snapshot payload could not be decrypted", ErrValidation)
	}
	var secrets []snapshotSecret
	if err := json.Unmarshal(raw, &secrets); err != nil {
		return 0, err
	}
	if len(secrets) > 5000 {
		return 0, fmt.Errorf("%w: snapshot contains too many secrets", ErrValidation)
	}
	message := "Restored from snapshot " + snapshot.CreatedAt.UTC().Format(time.RFC3339)
	restored := 0
	for _, secret := range secrets {
		in := SecretInput{
			Key: secret.Key, Value: secret.Value, Kind: secret.Kind, Comment: secret.Comment,
			Folder: secret.Folder, Notes: secret.Notes, ProviderURL: secret.ProviderURL,
			OwnerGroupID: secret.OwnerGroupID, Tags: secret.Tags, ChangeMessage: message,
		}
		if _, err := s.CreateSecret(ctx, actor, orgSlug, projectSlug, envSlug, in, meta); err != nil {
			if !errors.Is(err, ErrConflict) {
				return restored, err
			}
			if _, err := s.UpdateSecret(ctx, actor, orgSlug, projectSlug, envSlug, secret.Key, in, meta); err != nil {
				return restored, err
			}
		}
		restored++
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "env.restore",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Reason: snapshot.ID, Count: restored}, meta); err != nil {
		s.Logger.Error("audit env.restore failed", "snapshot_id", snapshot.ID, "error", err)
	}
	return restored, nil
}
