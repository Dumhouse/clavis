package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// PersonalTokenInput describes a personal access token.
type PersonalTokenInput struct {
	Permissions   []authz.Permission
	ProjectSlug   string
	Environment   string
	ExpiresInDays int
}

// PersonalTokenResult returns the one-time plaintext token.
type PersonalTokenResult struct {
	Token  string
	Record *store.AccessToken
}

type personalScopes struct {
	Permissions []string `json:"permissions"`
	ProjectID   string   `json:"project_id,omitempty"`
	Environment string   `json:"environment,omitempty"`
}

// CreatePersonalToken issues a scoped token for the calling user.
func (s *Service) CreatePersonalToken(ctx context.Context, actor authz.Actor, orgSlug string, in PersonalTokenInput, meta AuditMeta) (*PersonalTokenResult, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	permissions := in.Permissions
	if len(permissions) == 0 {
		permissions = []authz.Permission{authz.SecretsReadNames, authz.SecretsUse}
	}
	projectID := ""
	if in.ProjectSlug != "" {
		project, err := s.Store.GetProjectBySlug(ctx, org.ID, in.ProjectSlug)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		projectID = project.ID
	}
	environment := in.Environment
	if environment == "" {
		environment = "*"
	}
	scope := authz.Scope{OrgID: org.ID, ProjectID: projectID, Environment: environment}
	names := make([]string, 0, len(permissions))
	for _, perm := range permissions {
		if !perm.Valid() {
			return nil, fmt.Errorf("%w: unknown permission %q", ErrValidation, perm)
		}
		if !s.Engine.Authorize(actor, perm, scope, s.Now()).Allowed {
			return nil, fmt.Errorf("%w: you cannot grant a permission you do not hold", ErrForbidden)
		}
		names = append(names, string(perm))
	}
	days := in.ExpiresInDays
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	token, err := authn.GenerateToken(authn.TokenPersonal)
	if err != nil {
		return nil, err
	}
	rawScopes, err := json.Marshal(personalScopes{Permissions: names, ProjectID: projectID, Environment: environment})
	if err != nil {
		return nil, err
	}
	expires := s.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	record := &store.AccessToken{
		ID:        uuid.NewString(),
		OrgID:     org.ID,
		UserID:    actor.ID,
		TokenHash: authn.HashToken(token),
		Prefix:    authn.TokenPrefix(token),
		Scopes:    string(rawScopes),
		ExpiresAt: &expires,
		CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateAccessToken(ctx, record); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "token.create",
		audit.Resource{Type: "token", ID: record.ID, Name: record.Prefix},
		audit.Details{Environment: environment, Reason: fmt.Sprintf("%d permissions", len(names))}, meta); err != nil {
		s.Logger.Error("audit token.create failed", "token_id", record.ID, "error", err)
	}
	return &PersonalTokenResult{Token: token, Record: record}, nil
}

// ListPersonalTokens returns the caller's tokens in an org.
func (s *Service) ListPersonalTokens(ctx context.Context, actor authz.Actor, orgSlug string) ([]store.AccessToken, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return s.Store.ListPersonalTokens(ctx, actor.ID, org.ID)
}

// RevokePersonalToken revokes one of the caller's tokens.
func (s *Service) RevokePersonalToken(ctx context.Context, actor authz.Actor, orgSlug, tokenID string, meta AuditMeta) error {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Authorize(actor, authz.OrgRead, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	if err := s.Store.DeletePersonalToken(ctx, actor.ID, org.ID, tokenID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "token.revoke",
		audit.Resource{Type: "token", ID: tokenID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit token.revoke failed", "token_id", tokenID, "error", err)
	}
	return nil
}

// AuthenticatePersonalToken resolves a clv_pat_ token into a scoped actor.
func (s *Service) AuthenticatePersonalToken(ctx context.Context, raw, ip string) (authz.Actor, error) {
	kind, err := authn.ParseToken(raw)
	if err != nil || kind != authn.TokenPersonal {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	record, _, err := s.Store.GetAccessTokenByHash(ctx, authn.HashToken(raw))
	if err != nil || record.UserID == "" {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	now := s.Now().UTC()
	if record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	membership, err := s.Store.GetMembership(ctx, record.OrgID, record.UserID)
	if err != nil || membership.RemovedAt != nil || membership.Status != "active" {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	user, err := s.Store.GetUser(ctx, record.UserID)
	if err != nil {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	var scopes personalScopes
	if err := json.Unmarshal([]byte(record.Scopes), &scopes); err != nil {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	permissions := make([]authz.Permission, 0, len(scopes.Permissions))
	for _, name := range scopes.Permissions {
		permissions = append(permissions, authz.Permission(name))
	}
	actor := authz.Actor{
		ID: user.ID, Kind: authz.SubjectUser, Name: user.Name,
		Bindings: []authz.Binding{{
			ID: record.ID, Permissions: permissions,
			Scope: authz.Scope{OrgID: record.OrgID, ProjectID: scopes.ProjectID, Environment: scopes.Environment},
		}},
	}
	if err := s.Store.TouchAccessToken(ctx, record.ID, ip); err != nil {
		s.Logger.Warn("touch personal token failed", "token_id", record.ID, "error", err)
	}
	return actor, nil
}
