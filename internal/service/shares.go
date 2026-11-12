package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ShareLinkView is an encrypted share link without server-side key material.
type ShareLinkView struct {
	ID        string
	ExpiresAt time.Time
	MaxViews  int
}

// ShareClaim is the ciphertext returned to a share link visitor.
type ShareClaim struct {
	Ciphertext string
	Nonce      string
}

// CreateShare stores a client-encrypted share payload.
func (s *Service) CreateShare(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, envSlug, ciphertext, nonce string, ttl time.Duration, maxViews int, password string, meta AuditMeta) (*ShareLinkView, error) {
	org, project, env, err := s.resolveEnvironment(ctx, actor, orgSlug, projectSlug, envSlug)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeSecretAccess(actor, org, project, env, authz.SecretsReadValues); err != nil {
		return nil, err
	}
	if err := validateSharePayload(ciphertext, nonce, ttl, maxViews, password); err != nil {
		return nil, err
	}
	cipherBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("%w: ciphertext must be base64url", ErrValidation)
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(nonce))
	if err != nil {
		return nil, fmt.Errorf("%w: nonce must be base64url", ErrValidation)
	}
	var passwordHash string
	if password != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		passwordHash = string(hash)
	}
	combined := append(append([]byte(nil), nonceBytes...), cipherBytes...)
	expires := s.Now().UTC().Add(ttl)
	link := &store.ShareLink{
		ID: uuid.NewString(), Ciphertext: combined, PasswordHash: passwordHash,
		ViewLimit: maxViews, ExpiresAt: &expires, CreatedAt: s.Now().UTC(),
	}
	if err := s.Store.CreateShareLink(ctx, link); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "share.create",
		audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Count: maxViews, Channel: "share"}, meta); err != nil {
		s.Logger.Error("audit share.create failed", "share_id", link.ID, "error", err)
	}
	return &ShareLinkView{ID: link.ID, ExpiresAt: expires, MaxViews: maxViews}, nil
}

// ClaimShare validates expiry, view limit, and password, then consumes a view.
func (s *Service) ClaimShare(ctx context.Context, id, password string) (*ShareClaim, error) {
	link, err := s.Store.GetShareLink(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	now := s.Now().UTC()
	if link.ExpiresAt == nil || !now.Before(*link.ExpiresAt) || link.Views >= link.ViewLimit {
		return nil, ErrNotFound
	}
	if link.PasswordHash != "" {
		if err := bcrypt.CompareHashAndPassword([]byte(link.PasswordHash), []byte(password)); err != nil {
			return nil, ErrForbidden
		}
	}
	if err := s.Store.ConsumeShareLink(ctx, link.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(link.Ciphertext) < 13 {
		return nil, ErrNotFound
	}
	nonceBytes := link.Ciphertext[:12]
	cipherBytes := link.Ciphertext[12:]
	_ = s.Store.DeleteExpiredShareLinks(ctx, now)
	return &ShareClaim{
		Ciphertext: base64.RawURLEncoding.EncodeToString(cipherBytes),
		Nonce:      base64.RawURLEncoding.EncodeToString(nonceBytes),
	}, nil
}

func validateSharePayload(ciphertext, nonce string, ttl time.Duration, maxViews int, password string) error {
	if _, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(ciphertext)); err != nil {
		return fmt.Errorf("%w: ciphertext must be base64url", ErrValidation)
	}
	nonceBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(nonce))
	if err != nil || len(nonceBytes) != 12 {
		return fmt.Errorf("%w: nonce must be 12 bytes of base64url", ErrValidation)
	}
	if ttl < time.Hour || ttl > 30*24*time.Hour {
		return fmt.Errorf("%w: share lifetime must be between 1 hour and 30 days", ErrValidation)
	}
	if maxViews < 1 || maxViews > 100 {
		return fmt.Errorf("%w: view limit must be between 1 and 100", ErrValidation)
	}
	if len(password) > 256 {
		return fmt.Errorf("%w: password must be under 256 characters", ErrValidation)
	}
	return nil
}
