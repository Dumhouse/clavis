package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/coreos/go-oidc/v3/oidc"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// IdentityInput creates a machine or agent identity plus its first token.
type IdentityInput struct {
	Name          string
	Type          string
	Role          string
	ProjectSlug   string
	Environment   string
	ExpiresInDays int
	OIDC          *OIDCTrust
}

// OIDCTrust describes how a workload identity proves itself.
type OIDCTrust struct {
	Issuer   string            `json:"issuer"`
	Audience string            `json:"audience"`
	Claims   map[string]string `json:"claims,omitempty"`
}

// IdentityResult includes the one-time plaintext token.
type IdentityResult struct {
	Identity *store.Identity
	Token    string
	Record   *store.AccessToken
}

// TokenScope is the decoded scope of an identity token.
type TokenScope struct {
	Role        string
	ProjectID   string
	Environment string
}

// IdentityView is an identity with its token metadata.
type IdentityView struct {
	Identity *store.Identity
	Tokens   []store.AccessToken
	Scope    TokenScope
	OIDC     *OIDCTrust
}

type tokenScopes struct {
	Role        string `json:"role"`
	ProjectID   string `json:"project_id,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// CreateIdentity creates an identity and one scoped token.
func (s *Service) CreateIdentity(ctx context.Context, actor authz.Actor, orgSlug string, in IdentityInput, meta AuditMeta) (*IdentityResult, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	scope := authz.Scope{OrgID: org.ID}
	if err := s.Authorize(actor, authz.TokensManage, scope); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: identity name is required and must be under 100 characters", ErrValidation)
	}
	if in.Type == "" {
		in.Type = "machine"
	}
	if in.Type != "machine" && in.Type != "agent" {
		return nil, fmt.Errorf("%w: identity type must be machine or agent", ErrValidation)
	}
	if in.Role == "" {
		in.Role = authz.RoleDeveloper
	}
	if !s.CanGrantRole(ctx, actor, org.ID, in.Role, scope) {
		return nil, fmt.Errorf("%w: you cannot grant a role with more permissions than you hold", ErrForbidden)
	}
	if _, ok := s.RolePermissions(ctx, org.ID, in.Role); !ok {
		return nil, fmt.Errorf("%w: unknown role", ErrValidation)
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
	environment := strings.TrimSpace(in.Environment)
	if environment == "" {
		environment = "*"
	}
	ttlDays := in.ExpiresInDays
	if ttlDays <= 0 {
		ttlDays = 90
	}
	if ttlDays > 365 {
		ttlDays = 365
	}

	trustJSON := "{}"
	if in.OIDC != nil {
		if err := validateOIDCTrust(in.OIDC); err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(in.OIDC)
		if err != nil {
			return nil, err
		}
		trustJSON = string(encoded)
	}
	identity := &store.Identity{
		ID:              uuid.NewString(),
		OrgID:           org.ID,
		Name:            name,
		Type:            in.Type,
		TokenTTLSeconds: ttlDays * 24 * 3600,
		OIDCTrust:       trustJSON,
	}
	if err := s.Store.CreateIdentity(ctx, identity); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}

	token, record, err := s.newToken(ctx, identity, tokenScopes{
		Role: in.Role, ProjectID: projectID, Environment: environment,
	}, time.Duration(ttlDays)*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "identity.create",
		audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Environment: environment, Reason: in.Role}, meta); err != nil {
		s.Logger.Error("audit identity.create failed", "identity_id", identity.ID, "error", err)
	}
	return &IdentityResult{Identity: identity, Token: token, Record: record}, nil
}

func (s *Service) newToken(ctx context.Context, identity *store.Identity, scopes tokenScopes, ttl time.Duration) (string, *store.AccessToken, error) {
	kind := authn.TokenMachine
	if identity.Type == "agent" {
		kind = authn.TokenAgent
	}
	token, err := authn.GenerateToken(kind)
	if err != nil {
		return "", nil, err
	}
	rawScopes, err := json.Marshal(scopes)
	if err != nil {
		return "", nil, err
	}
	expires := s.Now().UTC().Add(ttl)
	record := &store.AccessToken{
		ID:         uuid.NewString(),
		OrgID:      identity.OrgID,
		IdentityID: identity.ID,
		TokenHash:  authn.HashToken(token),
		Prefix:     authn.TokenPrefix(token),
		Scopes:     string(rawScopes),
		ExpiresAt:  &expires,
		CreatedAt:  s.Now().UTC(),
	}
	if err := s.Store.CreateAccessToken(ctx, record); err != nil {
		return "", nil, err
	}
	return token, record, nil
}

// ListIdentities returns identities and their token metadata.
func (s *Service) ListIdentities(ctx context.Context, actor authz.Actor, orgSlug string) ([]IdentityView, error) {
	org, err := s.tokenOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	identities, err := s.Store.ListIdentities(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	out := make([]IdentityView, 0, len(identities))
	for i := range identities {
		tokens, err := s.Store.ListTokensForIdentity(ctx, identities[i].ID)
		if err != nil {
			return nil, err
		}
		scope := TokenScope{}
		if len(tokens) > 0 && tokens[0].Scopes != "" {
			var decoded tokenScopes
			if err := json.Unmarshal([]byte(tokens[0].Scopes), &decoded); err == nil {
				scope = TokenScope{Role: decoded.Role, ProjectID: decoded.ProjectID, Environment: decoded.Environment}
			}
		}
		view := IdentityView{Identity: &identities[i], Tokens: tokens, Scope: scope}
		if identities[i].OIDCTrust != "" && identities[i].OIDCTrust != "{}" {
			var trust OIDCTrust
			if json.Unmarshal([]byte(identities[i].OIDCTrust), &trust) == nil {
				view.OIDC = &trust
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// RotateIdentityToken revokes existing tokens and issues a new one with the same
// scope and a fresh expiry.
func (s *Service) RotateIdentityToken(ctx context.Context, actor authz.Actor, orgSlug, identityID string, expiresInDays int, meta AuditMeta) (*IdentityResult, error) {
	org, err := s.tokenOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	identity, err := s.Store.GetIdentity(ctx, org.ID, identityID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	existing, err := s.Store.ListTokensForIdentity(ctx, identity.ID)
	if err != nil {
		return nil, err
	}
	scopes := tokenScopes{Role: authz.RoleDeveloper, Environment: "*"}
	if len(existing) > 0 && existing[0].Scopes != "" {
		if err := json.Unmarshal([]byte(existing[0].Scopes), &scopes); err != nil {
			return nil, fmt.Errorf("service: decode token scopes: %w", err)
		}
	}
	if _, err := s.Store.DeleteTokensForIdentity(ctx, identity.ID); err != nil {
		return nil, err
	}
	days := expiresInDays
	if days <= 0 {
		days = 90
	}
	if days > 365 {
		days = 365
	}
	token, record, err := s.newToken(ctx, identity, scopes, time.Duration(days)*24*time.Hour)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "identity.rotate",
		audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Reason: scopes.Role}, meta); err != nil {
		s.Logger.Error("audit identity.rotate failed", "identity_id", identity.ID, "error", err)
	}
	return &IdentityResult{Identity: identity, Token: token, Record: record}, nil
}

// RevokeIdentity deletes an identity and all of its tokens.
func (s *Service) RevokeIdentity(ctx context.Context, actor authz.Actor, orgSlug, identityID string, meta AuditMeta) error {
	org, err := s.tokenOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteIdentity(ctx, org.ID, identityID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "identity.revoke",
		audit.Resource{Type: "identity", ID: identityID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit identity.revoke failed", "identity_id", identityID, "error", err)
	}
	return nil
}

// AuthenticateMachineToken resolves a clv_mt_/clv_at_ token into an actor.
func (s *Service) AuthenticateMachineToken(ctx context.Context, raw, ip string) (authz.Actor, error) {
	kind, err := authn.ParseToken(raw)
	if err != nil || (kind != authn.TokenMachine && kind != authn.TokenAgent) {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	record, identity, err := s.Store.GetAccessTokenByHash(ctx, authn.HashToken(raw))
	if err != nil || identity == nil {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	now := s.Now().UTC()
	if record.ExpiresAt != nil && !now.Before(*record.ExpiresAt) {
		return authz.Actor{}, authn.ErrUnauthenticated
	}
	var scopes tokenScopes
	if record.Scopes != "" {
		if err := json.Unmarshal([]byte(record.Scopes), &scopes); err != nil {
			return authz.Actor{}, authn.ErrUnauthenticated
		}
	}
	subjectKind := authz.SubjectMachine
	if identity.Type == "agent" {
		subjectKind = authz.SubjectAgent
	}
	actor := authz.Actor{
		ID:   identity.ID,
		Kind: subjectKind,
		Name: identity.Name,
		Bindings: []authz.Binding{{
			Role: scopes.Role,
			Scope: authz.Scope{
				OrgID:       record.OrgID,
				ProjectID:   scopes.ProjectID,
				Environment: scopes.Environment,
			},
		}},
	}
	if err := s.Store.TouchAccessToken(ctx, record.ID, ip); err != nil {
		s.Logger.Warn("touch access token failed", "token_id", record.ID, "error", err)
	}
	return actor, nil
}

func (s *Service) tokenOrg(ctx context.Context, actor authz.Actor, orgSlug string) (*store.Org, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.TokensManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return org, nil
}

func validateOIDCTrust(trust *OIDCTrust) error {
	trust.Issuer = strings.TrimRight(strings.TrimSpace(trust.Issuer), "/")
	trust.Audience = strings.TrimSpace(trust.Audience)
	if trust.Issuer == "" || trust.Audience == "" {
		return fmt.Errorf("%w: OIDC issuer and audience are required", ErrValidation)
	}
	if !strings.HasPrefix(trust.Issuer, "https://") && !strings.HasPrefix(trust.Issuer, "http://") {
		return fmt.Errorf("%w: OIDC issuer must be an http(s) URL", ErrValidation)
	}
	if len(trust.Claims) > 30 {
		return fmt.Errorf("%w: at most 30 claim conditions are allowed", ErrValidation)
	}
	return nil
}

// ExchangeOIDC verifies a workload JWT and issues a short-lived Clavis token.
func (s *Service) ExchangeOIDC(ctx context.Context, orgSlug, identityName, rawToken string, meta AuditMeta) (*IdentityResult, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	identity, err := s.Store.GetIdentityByName(ctx, org.ID, strings.TrimSpace(identityName))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var trust OIDCTrust
	if identity.OIDCTrust == "" || identity.OIDCTrust == "{}" {
		return nil, fmt.Errorf("%w: identity has no OIDC trust configured", ErrForbidden)
	}
	if err := json.Unmarshal([]byte(identity.OIDCTrust), &trust); err != nil {
		return nil, fmt.Errorf("%w: invalid OIDC trust configuration", ErrForbidden)
	}
	if trust.Issuer == "" || trust.Audience == "" || rawToken == "" {
		return nil, fmt.Errorf("%w: OIDC token rejected", ErrForbidden)
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, s.ssoHTTPClient()), trust.Issuer)
	if err != nil {
		return nil, fmt.Errorf("%w: OIDC issuer is unavailable", ErrSSOUnavailable)
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: trust.Audience}).Verify(ctx, rawToken)
	if err != nil {
		return nil, fmt.Errorf("%w: OIDC token rejected", ErrForbidden)
	}
	var claims map[string]any
	if err := verified.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: OIDC claims unavailable", ErrForbidden)
	}
	for claim, pattern := range trust.Claims {
		value, ok := claims[claim]
		if !ok {
			return nil, fmt.Errorf("%w: missing claim %s", ErrForbidden, claim)
		}
		text, ok := value.(string)
		if !ok || !authz.MatchGlob(pattern, text) {
			return nil, fmt.Errorf("%w: claim %s does not match", ErrForbidden, claim)
		}
	}
	scopes := tokenScopes{Role: authz.RoleDeveloper, Environment: "*"}
	tokens, err := s.Store.ListTokensForIdentity(ctx, identity.ID)
	if err != nil {
		return nil, err
	}
	if len(tokens) > 0 && tokens[0].Scopes != "" {
		_ = json.Unmarshal([]byte(tokens[0].Scopes), &scopes)
	}
	ttl := time.Duration(identity.TokenTTLSeconds) * time.Second
	if ttl <= 0 || ttl > time.Hour {
		ttl = time.Hour
	}
	if ttl < 5*time.Minute {
		ttl = 5 * time.Minute
	}
	token, record, err := s.newToken(ctx, identity, scopes, ttl)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(authz.Actor{ID: identity.ID, Kind: authz.SubjectMachine, Name: identity.Name}),
		org.ID, "identity.oidc_login", audit.Resource{Type: "identity", ID: identity.ID, Name: identity.Name},
		audit.Details{Environment: scopes.Environment, Reason: trust.Issuer}, meta); err != nil {
		s.Logger.Error("audit identity.oidc_login failed", "identity_id", identity.ID, "error", err)
	}
	return &IdentityResult{Identity: identity, Token: token, Record: record}, nil
}
