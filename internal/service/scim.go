package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

const scimTokenPrefix = "clv_scim_" //nolint:gosec // G101: token prefix, not a credential

// ScimTokenResult returns the one-time plaintext SCIM token.
type ScimTokenResult struct {
	Token  string
	Record *store.ScimToken
}

// CreateScimToken issues a bearer token for a SCIM provisioning client.
func (s *Service) CreateScimToken(ctx context.Context, actor authz.Actor, orgSlug string, meta AuditMeta) (*ScimTokenResult, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := scimTokenPrefix + base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	record := &store.ScimToken{
		ID: uuid.NewString(), OrgID: org.ID, TokenHash: hash[:],
		Prefix: token[:len(scimTokenPrefix)+8], CreatedBy: actor.ID,
	}
	if err := s.Store.CreateScimToken(ctx, record); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "scim.token.create",
		audit.Resource{Type: "scim_token", ID: record.ID, Name: record.Prefix}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit scim.token.create failed", "token_id", record.ID, "error", err)
	}
	return &ScimTokenResult{Token: token, Record: record}, nil
}

// ListScimTokens returns SCIM token metadata for an org.
func (s *Service) ListScimTokens(ctx context.Context, actor authz.Actor, orgSlug string) ([]store.ScimToken, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return s.Store.ListScimTokens(ctx, org.ID)
}

// RevokeScimToken removes a token.
func (s *Service) RevokeScimToken(ctx context.Context, actor authz.Actor, orgSlug, id string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.SettingsManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	if err := s.Store.DeleteScimToken(ctx, org.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "scim.token.revoke",
		audit.Resource{Type: "scim_token", ID: id}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit scim.token.revoke failed", "token_id", id, "error", err)
	}
	return nil
}

// AuthenticateScimToken resolves a bearer token into its organization.
func (s *Service) AuthenticateScimToken(ctx context.Context, raw string) (*store.Org, error) {
	if len(raw) < len(scimTokenPrefix) || raw[:len(scimTokenPrefix)] != scimTokenPrefix {
		return nil, authzErr()
	}
	hash := sha256.Sum256([]byte(raw))
	token, err := s.Store.GetScimTokenByHash(ctx, hash[:])
	if err != nil {
		return nil, authzErr()
	}
	if err := s.Store.TouchScimToken(ctx, token.ID); err != nil {
		s.Logger.Warn("touch scim token failed", "token_id", token.ID, "error", err)
	}
	org, err := s.Store.GetOrgByID(ctx, token.OrgID)
	if err != nil {
		return nil, authzErr()
	}
	return org, nil
}

func authzErr() error {
	return fmt.Errorf("%w: invalid SCIM token", ErrForbidden)
}
