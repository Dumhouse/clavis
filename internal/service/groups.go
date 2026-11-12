package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// GroupView is a group with its members.
type GroupView struct {
	Group   *store.Group
	Members []store.GroupMember
}

// CreateGroup creates an org group.
func (s *Service) CreateGroup(ctx context.Context, actor authz.Actor, orgSlug, name string, meta AuditMeta) (*store.Group, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: group name is required and must be under 100 characters", ErrValidation)
	}
	group := &store.Group{OrgID: org.ID, Name: name}
	if err := s.Store.CreateGroup(ctx, group); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "group.create",
		audit.Resource{Type: "group", ID: group.ID, Name: group.Name}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit group.create failed", "group_id", group.ID, "error", err)
	}
	return group, nil
}

// ListGroups returns groups with their members.
func (s *Service) ListGroups(ctx context.Context, actor authz.Actor, orgSlug string) ([]GroupView, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	groups, err := s.Store.ListGroups(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	out := make([]GroupView, 0, len(groups))
	for i := range groups {
		members, err := s.Store.ListGroupMembers(ctx, groups[i].ID)
		if err != nil {
			return nil, err
		}
		out = append(out, GroupView{Group: &groups[i], Members: members})
	}
	return out, nil
}

// RenameGroup changes a group's name.
func (s *Service) RenameGroup(ctx context.Context, actor authz.Actor, orgSlug, groupID, name string, meta AuditMeta) (*store.Group, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, fmt.Errorf("%w: group name is required", ErrValidation)
	}
	if err := s.Store.RenameGroup(ctx, org.ID, groupID, name); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrConflict
		}
		return nil, err
	}
	group, err := s.Store.GetGroup(ctx, org.ID, groupID)
	if err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "group.update",
		audit.Resource{Type: "group", ID: group.ID, Name: group.Name}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit group.update failed", "group_id", group.ID, "error", err)
	}
	return group, nil
}

// DeleteGroup removes a group.
func (s *Service) DeleteGroup(ctx context.Context, actor authz.Actor, orgSlug, groupID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteGroup(ctx, org.ID, groupID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "group.delete",
		audit.Resource{Type: "group", ID: groupID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit group.delete failed", "group_id", groupID, "error", err)
	}
	return nil
}

// AddGroupMember adds an org member to a group.
func (s *Service) AddGroupMember(ctx context.Context, actor authz.Actor, orgSlug, groupID, userID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if _, err := s.Store.GetGroup(ctx, org.ID, groupID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if _, err := s.Store.GetMembership(ctx, org.ID, userID); err != nil {
		return fmt.Errorf("%w: user is not a member of this organization", ErrValidation)
	}
	if err := s.Store.AddGroupMember(ctx, groupID, userID); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return ErrConflict
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "group.member.add",
		audit.Resource{Type: "group", ID: groupID}, audit.Details{Reason: userID}, meta); err != nil {
		s.Logger.Error("audit group.member.add failed", "group_id", groupID, "error", err)
	}
	return nil
}

// RemoveGroupMember removes a user from a group.
func (s *Service) RemoveGroupMember(ctx context.Context, actor authz.Actor, orgSlug, groupID, userID string, meta AuditMeta) error {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return err
	}
	if err := s.Store.RemoveGroupMember(ctx, groupID, userID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "group.member.remove",
		audit.Resource{Type: "group", ID: groupID}, audit.Details{Reason: userID}, meta); err != nil {
		s.Logger.Error("audit group.member.remove failed", "group_id", groupID, "error", err)
	}
	return nil
}
