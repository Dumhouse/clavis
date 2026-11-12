package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// Promotion statuses describe the target relative to the source.
const (
	PromotionAdded     = "added"
	PromotionChanged   = "changed"
	PromotionMissing   = "missing"
	PromotionUnchanged = "unchanged"
)

// PromotionEntry is one key in a promotion diff.
type PromotionEntry struct {
	Key             string
	Status          string
	Kind            string
	SourceKind      string
	TargetKind      string
	SchemaError     string
	SourceInherited bool
	TargetInherited bool
}

// PromotionPlan compares two environments.
type PromotionPlan struct {
	Source           string
	Target           string
	SourceType       string
	TargetType       string
	TargetProtected  bool
	ApprovalRequired bool
	Entries          []PromotionEntry
}

// PromotionRequestView is a pending protected-environment promotion.
type PromotionRequestView struct {
	ID          string
	Source      string
	Target      string
	Keys        []string
	Status      string
	RequestedBy string
	RequestedAt string
	DecidedBy   string
	DecidedAt   string
}

// PromotionResult reports an applied promotion.
type PromotionResult struct {
	Applied    int
	SnapshotID string
	Request    *PromotionRequestView
}

// PlanPromotion compares source and target values and schema checks.
func (s *Service) PlanPromotion(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, sourceSlug, targetSlug string, meta AuditMeta) (*PromotionPlan, error) {
	org, project, source, target, err := s.promotionEnvironments(ctx, actor, orgSlug, projectSlug, sourceSlug, targetSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, source, authz.SecretsReadValues); err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, target, authz.SecretsReadValues); err != nil {
		return nil, err
	}
	sourceSecrets, _, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, sourceSlug, true, "promote", meta)
	if err != nil {
		return nil, err
	}
	targetSecrets, _, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, targetSlug, true, "promote", meta)
	if err != nil {
		return nil, err
	}
	schema, err := s.schemaEntries(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	sourceMap := make(map[string]Secret, len(sourceSecrets))
	for _, secret := range sourceSecrets {
		sourceMap[secret.Key] = secret
	}
	targetMap := make(map[string]Secret, len(targetSecrets))
	for _, secret := range targetSecrets {
		targetMap[secret.Key] = secret
	}
	keySet := map[string]bool{}
	for key := range sourceMap {
		keySet[key] = true
	}
	for key := range targetMap {
		keySet[key] = true
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	entries := make([]PromotionEntry, 0, len(keys))
	for _, key := range keys {
		src, inSource := sourceMap[key]
		dst, inTarget := targetMap[key]
		entry := PromotionEntry{Key: key}
		switch {
		case !inSource:
			entry.Status = PromotionMissing
			entry.TargetKind = dst.Kind
			entry.TargetInherited = dst.Inherited
		case !inTarget:
			entry.Status = PromotionAdded
			entry.SourceKind = src.Kind
			entry.Kind = src.Kind
			entry.SourceInherited = src.Inherited
		case src.Value != dst.Value || src.Kind != dst.Kind:
			entry.Status = PromotionChanged
			entry.SourceKind = src.Kind
			entry.TargetKind = dst.Kind
			entry.Kind = src.Kind
			entry.SourceInherited = src.Inherited
			entry.TargetInherited = dst.Inherited
		default:
			entry.Status = PromotionUnchanged
			entry.SourceKind = src.Kind
			entry.TargetKind = dst.Kind
			entry.Kind = src.Kind
		}
		if inSource {
			if schemaEntry := schema[key]; schemaEntry != nil && src.Kind != "file" {
				if err := schemaEntry.ValidateValue(src.Value); err != nil {
					entry.SchemaError = promotionSchemaDetail(err)
				}
			}
		}
		entries = append(entries, entry)
	}
	approvalRequired := target.IsProtected &&
		!s.Engine.Authorize(actor, authz.SettingsManage, secretScope(org, project, target), s.Now()).Allowed
	return &PromotionPlan{
		Source: source.Slug, Target: target.Slug, SourceType: source.Type, TargetType: target.Type,
		TargetProtected: target.IsProtected, ApprovalRequired: approvalRequired, Entries: entries,
	}, nil
}

// Promote copies selected keys from source to target. Protected targets need an
// Admin/Owner; callers can use CreatePromotionRequest instead.
func (s *Service) Promote(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, sourceSlug, targetSlug string, keys []string, snapshot bool, message string, meta AuditMeta) (*PromotionResult, error) {
	org, project, _, target, err := s.promotionEnvironments(ctx, actor, orgSlug, projectSlug, sourceSlug, targetSlug)
	if err != nil {
		return nil, err
	}
	if target.IsProtected && !s.Engine.Authorize(actor, authz.SettingsManage, secretScope(org, project, target), s.Now()).Allowed {
		return nil, ErrPromotionApprovalRequired
	}
	applied, snapshotID, err := s.applyPromotion(ctx, actor, org, project, target, orgSlug, projectSlug, sourceSlug, targetSlug, keys, snapshot, message, meta)
	if err != nil {
		return nil, err
	}
	return &PromotionResult{Applied: applied, SnapshotID: snapshotID}, nil
}

func (s *Service) applyPromotion(ctx context.Context, actor authz.Actor, org *store.Org, project *store.Project, target *store.Environment, orgSlug, projectSlug, sourceSlug, targetSlug string, keys []string, snapshot bool, message string, meta AuditMeta) (int, string, error) {
	if len(keys) == 0 {
		return 0, "", fmt.Errorf("%w: select at least one key", ErrValidation)
	}
	if len(keys) > 1000 {
		return 0, "", fmt.Errorf("%w: at most 1000 keys can be promoted at once", ErrValidation)
	}
	if err := s.authorizeSecretAccess(actor, org, project, target, authz.SecretsWrite); err != nil {
		return 0, "", err
	}
	if err := s.enforceWritePolicy(ctx, org, target, meta); err != nil {
		return 0, "", err
	}
	sourceSecrets, _, err := s.ListSecrets(ctx, actor, orgSlug, projectSlug, sourceSlug, true, "promote", meta)
	if err != nil {
		return 0, "", err
	}
	sourceMap := make(map[string]Secret, len(sourceSecrets))
	for _, secret := range sourceSecrets {
		sourceMap[secret.Key] = secret
	}
	schema, err := s.schemaEntries(ctx, project.ID)
	if err != nil {
		return 0, "", err
	}
	selected := make([]Secret, 0, len(keys))
	seen := map[string]bool{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		sourceSecret, ok := sourceMap[key]
		if !ok {
			return 0, "", fmt.Errorf("%w: key %s does not exist in %s", ErrNotFound, key, sourceSlug)
		}
		if sourceSecret.Kind != "file" {
			if entry := schema[key]; entry != nil {
				if err := entry.ValidateValue(sourceSecret.Value); err != nil {
					return 0, "", fmt.Errorf("%w: %s", ErrValidation, promotionSchemaDetail(err))
				}
			}
		}
		selected = append(selected, sourceSecret)
	}
	if len(selected) == 0 {
		return 0, "", fmt.Errorf("%w: select at least one key", ErrValidation)
	}
	snapshotID := ""
	if snapshot {
		view, err := s.CreateSnapshot(ctx, actor, orgSlug, projectSlug, targetSlug, "Before promotion from "+sourceSlug, meta)
		if err != nil {
			return 0, "", err
		}
		snapshotID = view.ID
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = "Promoted from " + sourceSlug
	}
	applied := 0
	for _, secret := range selected {
		in := SecretInput{
			Key: secret.Key, Value: secret.Value, Kind: secret.Kind, Comment: secret.Comment,
			Folder: secret.Folder, Notes: secret.Notes, ProviderURL: secret.ProviderURL,
			OwnerGroupID: secret.OwnerGroupID, Tags: secret.Tags, ChangeMessage: message,
		}
		if _, err := s.UpdateSecret(ctx, actor, orgSlug, projectSlug, targetSlug, secret.Key, in, meta); err != nil {
			if errors.Is(err, ErrNotFound) {
				if _, err := s.CreateSecret(ctx, actor, orgSlug, projectSlug, targetSlug, in, meta); err != nil {
					return applied, snapshotID, err
				}
			} else {
				return applied, snapshotID, err
			}
		}
		applied++
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "secrets.promote",
		audit.Resource{Type: "environment", ID: target.ID, Name: target.Slug, ProjectID: project.ID, EnvID: target.ID},
		audit.Details{Environment: target.Slug, Count: applied, Reason: sourceSlug + " -> " + target.Slug}, meta); err != nil {
		s.Logger.Error("audit secrets.promote failed", "env_id", target.ID, "error", err)
	}
	return applied, snapshotID, nil
}

func (s *Service) promotionEnvironments(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, sourceSlug, targetSlug string) (*store.Org, *store.Project, *store.Environment, *store.Environment, error) {
	if sourceSlug == targetSlug {
		return nil, nil, nil, nil, fmt.Errorf("%w: source and target environments must differ", ErrValidation)
	}
	org, project, source, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, sourceSlug)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	target, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, targetSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, nil, err
	}
	return org, project, source, target, nil
}

// CreatePromotionRequest records a protected-environment promotion for approval.
func (s *Service) CreatePromotionRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, sourceSlug, targetSlug string, keys []string, meta AuditMeta) (*PromotionRequestView, error) {
	org, project, source, target, err := s.promotionEnvironments(ctx, actor, orgSlug, projectSlug, sourceSlug, targetSlug)
	if err != nil {
		return nil, err
	}
	if !target.IsProtected {
		return nil, fmt.Errorf("%w: target %s is not protected; promote directly", ErrValidation, target.Slug)
	}
	if err := s.authorizeSecretAccess(actor, org, project, source, authz.SecretsReadValues); err != nil {
		return nil, err
	}
	normalized, err := normalizePromotionKeys(keys)
	if err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(normalized)
	request := &store.PromotionRequest{
		ID: uuid.NewString(), OrgID: org.ID, ProjectID: project.ID,
		SourceEnv: source.Slug, TargetEnv: target.Slug, Keys: string(encoded),
		Status: "pending", RequestedBy: actor.ID, RequestedAt: s.Now().UTC(),
	}
	if err := s.Store.CreatePromotionRequest(ctx, request); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "promotion.request",
		audit.Resource{Type: "environment", ID: target.ID, Name: target.Slug, ProjectID: project.ID, EnvID: target.ID},
		audit.Details{Environment: target.Slug, Count: len(normalized), Reason: source.Slug}, meta); err != nil {
		s.Logger.Error("audit promotion.request failed", "request_id", request.ID, "error", err)
	}
	return promotionRequestView(request), nil
}

// ListPromotionRequests lists pending and decided requests.
func (s *Service) ListPromotionRequests(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, status string) ([]PromotionRequestView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsRead)
	if err != nil {
		return nil, err
	}
	_ = org
	requests, err := s.Store.ListPromotionRequests(ctx, project.ID, status)
	if err != nil {
		return nil, err
	}
	out := make([]PromotionRequestView, 0, len(requests))
	for i := range requests {
		out = append(out, *promotionRequestView(&requests[i]))
	}
	return out, nil
}

// ApprovePromotionRequest applies a pending request and records the decision.
func (s *Service) ApprovePromotionRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, requestID string, snapshot bool, meta AuditMeta) (*PromotionResult, error) {
	org, project, request, target, err := s.promotionRequestTarget(ctx, actor, orgSlug, projectSlug, requestID)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID, ProjectID: project.ID, Environment: target.Slug}); err != nil {
		return nil, err
	}
	keys := promotionRequestKeys(request)
	applied, snapshotID, err := s.applyPromotion(ctx, actor, org, project, target, orgSlug, projectSlug, request.SourceEnv, request.TargetEnv, keys, snapshot, "Approved promotion from "+request.SourceEnv, meta)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DecidePromotionRequest(ctx, project.ID, request.ID, "approved", actor.ID, s.Now().UTC()); err != nil {
		return nil, err
	}
	view := promotionRequestView(request)
	view.Status = "approved"
	view.DecidedBy = actor.ID
	return &PromotionResult{Applied: applied, SnapshotID: snapshotID, Request: view}, nil
}

// RejectPromotionRequest marks a request rejected.
func (s *Service) RejectPromotionRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, requestID string, meta AuditMeta) (*PromotionRequestView, error) {
	org, project, request, target, err := s.promotionRequestTarget(ctx, actor, orgSlug, projectSlug, requestID)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID, ProjectID: project.ID, Environment: target.Slug}); err != nil {
		return nil, err
	}
	if err := s.Store.DecidePromotionRequest(ctx, project.ID, request.ID, "rejected", actor.ID, s.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "promotion.reject",
		audit.Resource{Type: "environment", ID: target.ID, Name: target.Slug, ProjectID: project.ID, EnvID: target.ID},
		audit.Details{Environment: target.Slug, Reason: request.SourceEnv}, meta); err != nil {
		s.Logger.Error("audit promotion.reject failed", "request_id", request.ID, "error", err)
	}
	view := promotionRequestView(request)
	view.Status = "rejected"
	view.DecidedBy = actor.ID
	return view, nil
}

func (s *Service) promotionRequestTarget(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, requestID string) (*store.Org, *store.Project, *store.PromotionRequest, *store.Environment, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsRead)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	request, err := s.Store.GetPromotionRequest(ctx, project.ID, requestID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, nil, err
	}
	if request.Status != "pending" {
		return nil, nil, nil, nil, fmt.Errorf("%w: this promotion request is already %s", ErrConflict, request.Status)
	}
	target, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, request.TargetEnv)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, nil, nil, ErrNotFound
		}
		return nil, nil, nil, nil, err
	}
	return org, project, request, target, nil
}

func promotionSchemaDetail(err error) string {
	detail := err.Error()
	return strings.TrimPrefix(detail, "service: validation failed: ")
}

func normalizePromotionKeys(keys []string) ([]string, error) {
	if len(keys) == 0 || len(keys) > 1000 {
		return nil, fmt.Errorf("%w: select between 1 and 1000 keys", ErrValidation)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: select at least one key", ErrValidation)
	}
	return out, nil
}

func promotionRequestKeys(request *store.PromotionRequest) []string {
	var keys []string
	if err := json.Unmarshal([]byte(request.Keys), &keys); err != nil {
		return nil
	}
	return keys
}

func promotionRequestView(request *store.PromotionRequest) *PromotionRequestView {
	view := &PromotionRequestView{
		ID: request.ID, Source: request.SourceEnv, Target: request.TargetEnv,
		Keys: promotionRequestKeys(request), Status: request.Status,
		RequestedBy: request.RequestedBy, RequestedAt: request.RequestedAt.Format(time.RFC3339),
		DecidedBy: request.DecidedBy,
	}
	if request.DecidedAt != nil {
		view.DecidedAt = request.DecidedAt.Format(time.RFC3339)
	}
	return view
}
