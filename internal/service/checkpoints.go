package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// AuditVerifyResult reports chain and checkpoint integrity.
type AuditVerifyResult struct {
	ChainValid  bool
	Checkpoints int
	LastSeq     int64
	PublicKey   string
	Problem     string
}

// CreateAuditCheckpoint signs the current head of an org's audit chain.
func (s *Service) CreateAuditCheckpoint(ctx context.Context, actor authz.Actor, orgSlug string, meta AuditMeta) (*audit.Checkpoint, error) {
	if len(s.SigningKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("service: audit signing key is not configured")
	}
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	last, err := s.Store.LastAuditEvent(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	if last == nil {
		return nil, fmt.Errorf("%w: there are no audit events to checkpoint yet", ErrValidation)
	}
	checkpoint := &audit.Checkpoint{
		ID: uuid.NewString(), OrgID: org.ID, Seq: last.Seq, EventHash: last.Hash, CreatedAt: s.Now().UTC(),
	}
	if err := audit.SignCheckpoint(s.SigningKey, checkpoint); err != nil {
		return nil, err
	}
	if err := s.Store.CreateAuditCheckpoint(ctx, checkpoint); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "audit.checkpoint",
		audit.Resource{Type: "org", ID: org.ID, Name: org.Slug},
		audit.Details{Version: int(last.Seq)}, meta); err != nil {
		s.Logger.Error("audit checkpoint event failed", "org_id", org.ID, "error", err)
	}
	return checkpoint, nil
}

// ListAuditCheckpoints returns signed checkpoints for an org.
func (s *Service) ListAuditCheckpoints(ctx context.Context, actor authz.Actor, orgSlug string) ([]audit.Checkpoint, error) {
	org, err := s.auditOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	return s.Store.ListAuditCheckpoints(ctx, org.ID)
}

// VerifyAudit recomputes the hash chain and validates every signed checkpoint.
func (s *Service) VerifyAudit(ctx context.Context, actor authz.Actor, orgSlug string) (*AuditVerifyResult, error) {
	org, err := s.auditOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.ListAuditEvents(ctx, org.ID, 0, 0)
	if err != nil {
		return nil, err
	}
	checkpoints, err := s.Store.ListAuditCheckpoints(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	result := &AuditVerifyResult{Checkpoints: len(checkpoints)}
	if len(events) > 0 {
		result.LastSeq = events[len(events)-1].Seq
	}
	if len(s.SigningKey) == ed25519.PrivateKeySize {
		if pub, ok := s.SigningKey.Public().(ed25519.PublicKey); ok {
			result.PublicKey = base64.StdEncoding.EncodeToString(pub)
		}
	}
	if err := audit.Verify(events); err != nil {
		result.ChainValid = false
		result.Problem = err.Error()
		return result, nil
	}
	hashes := make(map[int64]string, len(events))
	for _, event := range events {
		hashes[event.Seq] = event.Hash
	}
	for i := range checkpoints {
		checkpoint := &checkpoints[i]
		if err := audit.VerifyCheckpoint(checkpoint); err != nil {
			result.ChainValid = false
			result.Problem = fmt.Sprintf("checkpoint #%d: %v", checkpoint.Seq, err)
			return result, nil
		}
		if hash, ok := hashes[checkpoint.Seq]; !ok || strings.TrimSpace(hash) != checkpoint.EventHash {
			result.ChainValid = false
			result.Problem = fmt.Sprintf("checkpoint #%d does not match the chain", checkpoint.Seq)
			return result, nil
		}
	}
	result.ChainValid = true
	return result, nil
}

func (s *Service) auditOrg(ctx context.Context, actor authz.Actor, orgSlug string) (*store.Org, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.AuditRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return org, nil
}
