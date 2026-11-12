package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

var (
	// ErrInviteInvalid covers expired, used, or unknown invite links.
	ErrInviteInvalid = errors.New("service: invite invalid")
	// ErrInviteEmailMismatch means the link was issued for another address.
	ErrInviteEmailMismatch = errors.New("service: invite email mismatch")
	// ErrLastOwner prevents an org from losing its final owner.
	ErrLastOwner = errors.New("service: cannot remove the last owner")
)

// InviteResult is returned when an invite is created. Token is shown once.
type InviteResult struct {
	Invite *store.Invite
	Token  string
}

// RolePermissions resolves the permissions of a built-in or custom role.
func (s *Service) RolePermissions(ctx context.Context, orgID, roleName string) ([]authz.Permission, bool) {
	if perms, ok := authz.BuiltinRolePermissions(roleName); ok {
		return perms, true
	}
	role, err := s.Store.GetRoleByName(ctx, orgID, roleName)
	if err != nil {
		return nil, false
	}
	return parsePermissions(role.Permissions), true
}

// CanGrantRole reports whether actor holds every permission of roleName.
func (s *Service) CanGrantRole(ctx context.Context, actor authz.Actor, orgID, roleName string, scope authz.Scope) bool {
	perms, ok := s.RolePermissions(ctx, orgID, roleName)
	if !ok {
		return false
	}
	for _, perm := range perms {
		if !s.Engine.Authorize(actor, perm, scope, s.Now()).Allowed {
			return false
		}
	}
	return true
}

// CreateInvite creates a shareable link for a role.
func (s *Service) CreateInvite(ctx context.Context, actor authz.Actor, orgSlug, email, roleName string, ttl time.Duration, meta AuditMeta) (*InviteResult, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	scope := authz.Scope{OrgID: org.ID}
	if err := s.Authorize(actor, authz.MembersManage, scope); err != nil {
		return nil, err
	}
	if roleName == "" {
		roleName = authz.RoleDeveloper
	}
	if !s.CanGrantRole(ctx, actor, org.ID, roleName, scope) {
		return nil, fmt.Errorf("%w: you cannot grant a role with more permissions than you hold", ErrForbidden)
	}
	role, err := s.Store.GetRoleByName(ctx, org.ID, roleName)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("%w: role must be Owner, Admin, Developer, or Viewer", ErrValidation)
		}
		return nil, err
	}
	if email != "" {
		email, err = authn.NormalizeEmail(email)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrValidation, err)
		}
	}
	if ttl <= 0 || ttl > 30*24*time.Hour {
		ttl = 7 * 24 * time.Hour
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(token))
	invite := &store.Invite{
		ID:        uuid.NewString(),
		OrgID:     org.ID,
		Email:     email,
		RoleID:    role.ID,
		RoleName:  role.Name,
		TokenHash: hash[:],
		CreatedBy: actor.ID,
		CreatedAt: s.Now().UTC(),
		ExpiresAt: s.Now().UTC().Add(ttl),
	}
	if err := s.Store.CreateInvite(ctx, invite); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "invite.create",
		audit.Resource{Type: "invite", ID: invite.ID, Name: role.Name},
		audit.Details{Reason: role.Name}, meta); err != nil {
		s.Logger.Error("audit invite.create failed", "invite_id", invite.ID, "error", err)
	}
	return &InviteResult{Invite: invite, Token: token}, nil
}

// ListInvites returns pending invites.
func (s *Service) ListInvites(ctx context.Context, actor authz.Actor, orgSlug string) ([]store.Invite, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	return s.Store.ListPendingInvites(ctx, org.ID)
}

// RevokeInvite deletes a pending invite.
func (s *Service) RevokeInvite(ctx context.Context, actor authz.Actor, orgSlug, inviteID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteInvite(ctx, org.ID, inviteID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "invite.revoke",
		audit.Resource{Type: "invite", ID: inviteID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit invite.revoke failed", "invite_id", inviteID, "error", err)
	}
	return nil
}

// InspectInvite resolves a raw token for the public join page.
func (s *Service) InspectInvite(ctx context.Context, token string) (*store.Invite, *store.Org, error) {
	invite, err := s.inviteByToken(ctx, token)
	if err != nil {
		return nil, nil, err
	}
	org, err := s.Store.GetOrgByID(ctx, invite.OrgID)
	if err != nil {
		return nil, nil, ErrInviteInvalid
	}
	return invite, org, nil
}

// AcceptInviteNewUser creates an account from a link and adds the membership.
func (s *Service) AcceptInviteNewUser(ctx context.Context, token string, user *store.User, meta AuditMeta) (*store.Org, error) {
	invite, org, err := s.InspectInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	if invite.Email != "" && !strings.EqualFold(invite.Email, user.Email) {
		return nil, ErrInviteEmailMismatch
	}
	appearance := ParseOrgSettings(org.Settings).DefaultAppearance
	if appearance != "warm" {
		appearance = "classic"
	}
	preferences, _ := json.Marshal(map[string]string{"theme": "system", "appearance": appearance})
	user.Preferences = string(preferences)
	if err := s.Store.AcceptInvite(ctx, invite.ID, user, invite.RoleID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name},
		org.ID, "invite.accept", audit.Resource{Type: "invite", ID: invite.ID, Name: invite.RoleName},
		audit.Details{Reason: invite.RoleName}, meta); err != nil {
		s.Logger.Error("audit invite.accept failed", "invite_id", invite.ID, "error", err)
	}
	return org, nil
}

// AcceptInviteExisting adds the signed-in user to the org.
func (s *Service) AcceptInviteExisting(ctx context.Context, token string, user *store.User, meta AuditMeta) (*store.Org, error) {
	invite, org, err := s.InspectInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	if invite.Email != "" && !strings.EqualFold(invite.Email, user.Email) {
		return nil, ErrInviteEmailMismatch
	}
	if err := s.Store.AddMembershipFromInvite(ctx, invite.ID, user.ID, invite.RoleID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, audit.Actor{ID: user.ID, Kind: string(authz.SubjectUser), Name: user.Name},
		org.ID, "invite.accept", audit.Resource{Type: "invite", ID: invite.ID, Name: invite.RoleName},
		audit.Details{Reason: invite.RoleName}, meta); err != nil {
		s.Logger.Error("audit invite.accept failed", "invite_id", invite.ID, "error", err)
	}
	return org, nil
}

func (s *Service) inviteByToken(ctx context.Context, token string) (*store.Invite, error) {
	if token == "" || len(token) > 256 {
		return nil, ErrInviteInvalid
	}
	hash := sha256.Sum256([]byte(token))
	invite, err := s.Store.GetInviteByTokenHash(ctx, hash[:])
	if err != nil {
		return nil, ErrInviteInvalid
	}
	if invite.AcceptedAt != nil || !s.Now().UTC().Before(invite.ExpiresAt) {
		return nil, ErrInviteInvalid
	}
	return invite, nil
}

// ListMembers returns active members.
func (s *Service) ListMembers(ctx context.Context, actor authz.Actor, orgSlug string) ([]store.Member, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	return s.Store.ListMembers(ctx, org.ID)
}

// UpdateMemberRole changes a member's role, preventing privilege escalation and
// removal of the last owner.
func (s *Service) UpdateMemberRole(ctx context.Context, actor authz.Actor, orgSlug, userID, roleName string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	scope := authz.Scope{OrgID: org.ID}
	if !s.CanGrantRole(ctx, actor, org.ID, roleName, scope) {
		return fmt.Errorf("%w: you cannot grant a role with more permissions than you hold", ErrForbidden)
	}
	role, err := s.Store.GetRoleByName(ctx, org.ID, roleName)
	if err != nil {
		return fmt.Errorf("%w: unknown role", ErrValidation)
	}
	membership, err := s.Store.GetMembership(ctx, org.ID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if membership.RoleName == authz.RoleOwner && role.Name != authz.RoleOwner {
		owners, err := s.Store.CountMembersWithRole(ctx, org.ID, authz.RoleOwner)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if err := s.Store.UpdateMemberRole(ctx, org.ID, userID, role.ID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "member.update",
		audit.Resource{Type: "member", ID: userID}, audit.Details{Reason: role.Name}, meta); err != nil {
		s.Logger.Error("audit member.update failed", "user_id", userID, "error", err)
	}
	return nil
}

// RemoveMember removes a member and revokes their sessions and scoped grants.
func (s *Service) RemoveMember(ctx context.Context, actor authz.Actor, orgSlug, userID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	membership, err := s.Store.GetMembership(ctx, org.ID, userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if membership.RoleName == authz.RoleOwner {
		owners, err := s.Store.CountMembersWithRole(ctx, org.ID, authz.RoleOwner)
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	if err := s.createOffboardingTasks(ctx, org.ID, userID); err != nil {
		s.Logger.Warn("create offboarding tasks failed", "user_id", userID, "error", err)
	}
	if err := s.Store.RemoveMember(ctx, org.ID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.Store.DeleteUserPersonalTokens(ctx, userID, org.ID); err != nil {
		s.Logger.Error("revoke removed member personal tokens failed", "user_id", userID, "error", err)
	}
	if _, err := s.Store.DeleteUserSessions(ctx, userID); err != nil {
		s.Logger.Error("revoke removed member sessions failed", "user_id", userID, "error", err)
	}
	if err := s.Store.DeleteUserBindingsForOrg(ctx, org.ID, userID); err != nil {
		s.Logger.Error("delete removed member bindings failed", "user_id", userID, "error", err)
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "member.remove",
		audit.Resource{Type: "member", ID: userID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit member.remove failed", "user_id", userID, "error", err)
	}
	return nil
}

func (s *Service) membershipOrg(ctx context.Context, actor authz.Actor, orgSlug string) (*store.Org, error) {
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := s.Authorize(actor, authz.MembersManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	return org, nil
}
