package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// AccessRequestView is a just-in-time access request.
type AccessRequestView struct {
	ID                string
	ProjectID         string
	ProjectSlug       string
	RequesterID       string
	Environment       string
	Role              string
	Reason            string
	Status            string
	DurationSeconds   int
	RequiredApprovals int
	Approvals         []string
	ExpiresAt         *time.Time
	DecidedAt         *time.Time
	AppliedBindingID  string
	CreatedAt         time.Time
}

// CreateAccessRequest asks for temporary project/environment access.
func (s *Service) CreateAccessRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, role, reason string, durationSeconds, requiredApprovals int, meta AuditMeta) (*AccessRequestView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.EnvironmentsRead, secretScope(org, project, env)); err != nil {
		return nil, err
	}
	role = strings.TrimSpace(role)
	if role == "" {
		role = authz.RoleDeveloper
	}
	if _, err := s.Store.GetRoleByName(ctx, org.ID, role); err != nil {
		return nil, fmt.Errorf("%w: unknown role", ErrValidation)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 1024 {
		return nil, fmt.Errorf("%w: a reason under 1024 characters is required", ErrValidation)
	}
	if durationSeconds <= 0 {
		durationSeconds = 3600
	}
	if durationSeconds < 300 || durationSeconds > 86400 {
		return nil, fmt.Errorf("%w: access duration must be between 5 minutes and 24 hours", ErrValidation)
	}
	if requiredApprovals <= 0 {
		requiredApprovals = 1
	}
	if requiredApprovals > 5 {
		requiredApprovals = 5
	}
	now := s.Now().UTC()
	expires := now.Add(time.Duration(durationSeconds) * time.Second)
	request := &store.AccessRequest{
		ID: uuid.NewString(), OrgID: org.ID, ProjectID: project.ID, RequesterID: actor.ID,
		Environment: env.Slug, Role: role, Reason: reason, Status: "pending",
		DurationSeconds: durationSeconds, RequiredApprovals: requiredApprovals,
		Approvals: "[]", ExpiresAt: &expires, CreatedAt: now,
	}
	if err := s.Store.CreateAccessRequest(ctx, request); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.request.create",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Environment: env.Slug, Reason: reason}, meta); err != nil {
		s.Logger.Error("audit access.request.create failed", "request_id", request.ID, "error", err)
	}
	return accessRequestView(request, project.Slug), nil
}

// ListAccessRequests returns requests. Non-managers see only their own.
func (s *Service) ListAccessRequests(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, status string) ([]AccessRequestView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.EnvironmentsRead)
	if err != nil {
		return nil, err
	}
	requesterID := ""
	if !s.Engine.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID, ProjectID: project.ID}, s.Now()).Allowed {
		requesterID = actor.ID
	}
	requests, err := s.Store.ListAccessRequests(ctx, project.ID, status, requesterID)
	if err != nil {
		return nil, err
	}
	out := make([]AccessRequestView, 0, len(requests))
	for i := range requests {
		out = append(out, *accessRequestView(&requests[i], project.Slug))
	}
	return out, nil
}

// ApproveAccessRequest records an approval and grants access when enough exist.
func (s *Service) ApproveAccessRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, requestID string, meta AuditMeta) (*AccessRequestView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.RolesManage)
	if err != nil {
		return nil, err
	}
	request, err := s.Store.GetAccessRequest(ctx, project.ID, requestID)
	if err != nil {
		return nil, ErrNotFound
	}
	if request.Status != "pending" {
		return nil, fmt.Errorf("%w: request is already %s", ErrConflict, request.Status)
	}
	if request.RequesterID == actor.ID {
		return nil, fmt.Errorf("%w: you cannot approve your own request", ErrForbidden)
	}
	if request.ExpiresAt != nil && !s.Now().UTC().Before(*request.ExpiresAt) {
		return nil, fmt.Errorf("%w: request has expired", ErrConflict)
	}
	var approvals []string
	if err := json.Unmarshal([]byte(request.Approvals), &approvals); err != nil {
		approvals = nil
	}
	for _, approver := range approvals {
		if approver == actor.ID {
			return accessRequestView(request, project.Slug), nil
		}
	}
	approvals = append(approvals, actor.ID)
	encoded, _ := json.Marshal(approvals)
	request.Approvals = string(encoded)
	if len(approvals) >= request.RequiredApprovals {
		binding, err := s.GrantProjectAccess(ctx, actor, orgSlug, projectSlug, ProjectGrant{
			SubjectKind: "user", SubjectID: request.RequesterID, Role: request.Role,
			Environment: request.Environment, ExpiresInSeconds: request.DurationSeconds,
		}, meta)
		if err != nil {
			return nil, err
		}
		now := s.Now().UTC()
		request.Status = "approved"
		request.DecidedAt = &now
		request.AppliedBindingID = binding.ID
		expires := now.Add(time.Duration(request.DurationSeconds) * time.Second)
		request.ExpiresAt = &expires
	}
	if err := s.Store.UpdateAccessRequest(ctx, request); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.request.approve",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Environment: request.Environment, Reason: request.Role, Count: len(approvals)}, meta); err != nil {
		s.Logger.Error("audit access.request.approve failed", "request_id", request.ID, "error", err)
	}
	return accessRequestView(request, project.Slug), nil
}

// RejectAccessRequest closes a pending request.
func (s *Service) RejectAccessRequest(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, requestID string, meta AuditMeta) (*AccessRequestView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.RolesManage)
	if err != nil {
		return nil, err
	}
	request, err := s.Store.GetAccessRequest(ctx, project.ID, requestID)
	if err != nil {
		return nil, ErrNotFound
	}
	if request.Status != "pending" {
		return nil, fmt.Errorf("%w: request is already %s", ErrConflict, request.Status)
	}
	now := s.Now().UTC()
	request.Status = "rejected"
	request.DecidedAt = &now
	if err := s.Store.UpdateAccessRequest(ctx, request); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.request.reject",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Environment: request.Environment, Reason: request.Reason}, meta); err != nil {
		s.Logger.Error("audit access.request.reject failed", "request_id", request.ID, "error", err)
	}
	return accessRequestView(request, project.Slug), nil
}

func accessRequestView(request *store.AccessRequest, projectSlug string) *AccessRequestView {
	var approvals []string
	_ = json.Unmarshal([]byte(request.Approvals), &approvals)
	return &AccessRequestView{
		ID: request.ID, ProjectID: request.ProjectID, ProjectSlug: projectSlug, RequesterID: request.RequesterID,
		Environment: request.Environment, Role: request.Role, Reason: request.Reason,
		Status: request.Status, DurationSeconds: request.DurationSeconds,
		RequiredApprovals: request.RequiredApprovals, Approvals: approvals,
		ExpiresAt: request.ExpiresAt, DecidedAt: request.DecidedAt,
		AppliedBindingID: request.AppliedBindingID, CreatedAt: request.CreatedAt,
	}
}
