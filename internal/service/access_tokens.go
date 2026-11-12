package service

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/authz"
)

type accessApprovalPayload struct {
	RequestID string `json:"request_id"`
	ProjectID string `json:"project_id"`
	Action    string `json:"action"`
	ExpiresAt int64  `json:"exp"`
}

// SignAccessApproval creates a short-lived signed approve/reject token.
func (s *Service) SignAccessApproval(requestID, projectID, action string, ttl time.Duration) (string, error) {
	if len(s.SigningKey) == 0 {
		return "", fmt.Errorf("%w: signing key unavailable", ErrValidation)
	}
	if action != "approve" && action != "reject" {
		return "", fmt.Errorf("%w: action must be approve or reject", ErrValidation)
	}
	payload, err := json.Marshal(accessApprovalPayload{
		RequestID: requestID, ProjectID: projectID, Action: action,
		ExpiresAt: s.Now().UTC().Add(ttl).Unix(),
	})
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(s.SigningKey, payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(signature), nil
}

// ApplyAccessApproval verifies a signed token and applies the decision.
func (s *Service) ApplyAccessApproval(ctx context.Context, actor authz.Actor, token string, meta AuditMeta) (*AccessRequestView, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: invalid approval token", ErrForbidden)
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("%w: invalid approval token", ErrForbidden)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(s.SigningKey) == 0 || !ed25519.Verify(s.SigningKey.Public().(ed25519.PublicKey), payload, signature) {
		return nil, fmt.Errorf("%w: invalid approval token", ErrForbidden)
	}
	var decoded accessApprovalPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return nil, fmt.Errorf("%w: invalid approval token", ErrForbidden)
	}
	if s.Now().UTC().Unix() > decoded.ExpiresAt {
		return nil, fmt.Errorf("%w: approval token expired", ErrForbidden)
	}
	request, err := s.Store.GetAccessRequest(ctx, decoded.ProjectID, decoded.RequestID)
	if err != nil {
		return nil, ErrNotFound
	}
	project, err := s.Store.GetProjectByID(ctx, decoded.ProjectID)
	if err != nil {
		return nil, ErrNotFound
	}
	org, err := s.Store.GetOrgByID(ctx, project.OrgID)
	if err != nil {
		return nil, ErrNotFound
	}
	if decoded.Action == "approve" {
		return s.ApproveAccessRequest(ctx, actor, org.Slug, project.Slug, request.ID, meta)
	}
	return s.RejectAccessRequest(ctx, actor, org.Slug, project.Slug, request.ID, meta)
}

// AccessRequestApproverEmails returns Admin/Owner emails for notifications.
func (s *Service) AccessRequestApproverEmails(ctx context.Context, orgSlug string) ([]string, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		return nil, ErrNotFound
	}
	members, err := s.Store.ListMembers(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	var emails []string
	for _, member := range members {
		if member.RoleName != authz.RoleOwner && member.RoleName != authz.RoleAdmin {
			continue
		}
		emails = append(emails, member.Email)
	}
	return emails, nil
}
