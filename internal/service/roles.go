package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// RoleInput describes a custom role.
type RoleInput struct {
	Name        string
	Permissions []authz.Permission
}

// RoleView is a role plus its resolved permissions.
type RoleView struct {
	Role        *store.Role
	Permissions []authz.Permission
}

// ListRoles returns built-in and custom roles for an org.
func (s *Service) ListRoles(ctx context.Context, actor authz.Actor, orgSlug string) ([]RoleView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	roles, err := s.Store.ListRoles(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(roles))
	for i := range roles {
		view := RoleView{Role: &roles[i]}
		if roles[i].Builtin {
			view.Permissions, _ = authz.BuiltinRolePermissions(roles[i].Name)
		} else {
			view.Permissions = parsePermissions(roles[i].Permissions)
		}
		out = append(out, view)
	}
	return out, nil
}

// CreateRole adds a custom role.
func (s *Service) CreateRole(ctx context.Context, actor authz.Actor, orgSlug string, in RoleInput, meta AuditMeta) (*RoleView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	scope := authz.Scope{OrgID: org.ID}
	if err := s.Authorize(actor, authz.RolesManage, scope); err != nil {
		return nil, err
	}
	name, rawPermissions, err := s.validateRole(actor, org.ID, in, scope)
	if err != nil {
		return nil, err
	}
	role := &store.Role{
		ID:          uuid.NewString(),
		OrgID:       org.ID,
		Name:        name,
		Permissions: rawPermissions,
	}
	if err := s.Store.CreateRole(ctx, role); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "role.create",
		audit.Resource{Type: "role", ID: role.ID, Name: role.Name},
		audit.Details{Reason: rawPermissions}, meta); err != nil {
		s.Logger.Error("audit role.create failed", "role_id", role.ID, "error", err)
	}
	return &RoleView{Role: role, Permissions: in.Permissions}, nil
}

// UpdateRole edits a custom role.
func (s *Service) UpdateRole(ctx context.Context, actor authz.Actor, orgSlug, roleID string, in RoleInput, meta AuditMeta) (*RoleView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	scope := authz.Scope{OrgID: org.ID}
	if err := s.Authorize(actor, authz.RolesManage, scope); err != nil {
		return nil, err
	}
	existing, err := s.Store.GetRole(ctx, org.ID, roleID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if existing.Builtin {
		return nil, fmt.Errorf("%w: built-in roles cannot be edited", ErrForbidden)
	}
	name, rawPermissions, err := s.validateRole(actor, org.ID, in, scope)
	if err != nil {
		return nil, err
	}
	if err := s.Store.UpdateRole(ctx, org.ID, roleID, name, rawPermissions); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	existing.Name = name
	existing.Permissions = rawPermissions
	if err := s.record(ctx, toAuditActor(actor), org.ID, "role.update",
		audit.Resource{Type: "role", ID: roleID, Name: name},
		audit.Details{Reason: rawPermissions}, meta); err != nil {
		s.Logger.Error("audit role.update failed", "role_id", roleID, "error", err)
	}
	return &RoleView{Role: existing, Permissions: in.Permissions}, nil
}

// DeleteRole removes an unassigned custom role.
func (s *Service) DeleteRole(ctx context.Context, actor authz.Actor, orgSlug, roleID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return err
	}
	existing, err := s.Store.GetRole(ctx, org.ID, roleID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if existing.Builtin {
		return fmt.Errorf("%w: built-in roles cannot be deleted", ErrForbidden)
	}
	count, err := s.Store.CountMembersWithRoleID(ctx, org.ID, roleID)
	if err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: role is still assigned to %d member(s)", ErrConflict, count)
	}
	if err := s.Store.DeleteRole(ctx, org.ID, roleID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "role.delete",
		audit.Resource{Type: "role", ID: roleID, Name: existing.Name}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit role.delete failed", "role_id", roleID, "error", err)
	}
	return nil
}

// validateRole checks the name, permissions, and no-escalation rule.
func (s *Service) validateRole(actor authz.Actor, orgID string, in RoleInput, scope authz.Scope) (string, string, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > 60 {
		return "", "", fmt.Errorf("%w: role name is required and must be under 60 characters", ErrValidation)
	}
	for _, builtin := range []string{authz.RoleOwner, authz.RoleAdmin, authz.RoleDeveloper, authz.RoleViewer} {
		if strings.EqualFold(name, builtin) {
			return "", "", fmt.Errorf("%w: role name %q is reserved", ErrValidation, builtin)
		}
	}
	if len(in.Permissions) == 0 {
		return "", "", fmt.Errorf("%w: a role needs at least one permission", ErrValidation)
	}
	seen := make(map[authz.Permission]struct{}, len(in.Permissions))
	names := make([]string, 0, len(in.Permissions))
	for _, perm := range in.Permissions {
		if !perm.Valid() {
			return "", "", fmt.Errorf("%w: unknown permission %q", ErrValidation, perm)
		}
		if _, ok := seen[perm]; ok {
			continue
		}
		seen[perm] = struct{}{}
		if !s.Engine.Authorize(actor, perm, scope, s.Now()).Allowed {
			return "", "", fmt.Errorf("%w: you cannot grant a permission you do not hold", ErrForbidden)
		}
		names = append(names, string(perm))
	}
	raw, err := json.Marshal(names)
	if err != nil {
		return "", "", err
	}
	return name, string(raw), nil
}
