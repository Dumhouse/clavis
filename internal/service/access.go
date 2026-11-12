package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// MatrixInput narrows the access matrix to a project and environment.
type MatrixInput struct {
	ProjectSlug string
	Environment string
}

// MatrixRow is one subject's effective permissions in a scope.
type MatrixRow struct {
	SubjectKind string
	SubjectID   string
	Name        string
	Email       string
	Role        string
	MemberCount int
	Permissions []string
}

// AccessMatrix is the subject × permission grid.
type AccessMatrix struct {
	Permissions []string
	Scope       MatrixInput
	ProjectID   string
	Rows        []MatrixRow
}

// ExplainInput selects a subject and requested action.
type ExplainInput struct {
	SubjectKind string
	SubjectID   string
	Permission  string
	ProjectSlug string
	Environment string
	Path        string
}

// BindingView describes one grant considered by the explainer.
type BindingView struct {
	Role        string
	Source      string
	SubjectKind string
	SubjectID   string
	ProjectID   string
	Environment string
	Path        string
	ExpiresAt   *time.Time
	Grants      bool
}

// AccessExplanation says why a subject does or does not have access.
type AccessExplanation struct {
	Allowed   bool
	Reason    string
	Subject   string
	Scope     ExplainInput
	ProjectID string
	Matched   *BindingView
	Bindings  []BindingView
}

// AccessMatrix builds an effective-permission matrix for every member and group.
func (s *Service) AccessMatrix(ctx context.Context, actor authz.Actor, orgSlug string, in MatrixInput) (*AccessMatrix, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	scope := authz.Scope{OrgID: org.ID, Environment: in.Environment}
	if scope.Environment == "" {
		scope.Environment = "*"
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
		scope.ProjectID = project.ID
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	permissions := authz.AllPermissions()
	out := &AccessMatrix{Permissions: make([]string, 0, len(permissions)), Scope: in, ProjectID: projectID}
	for _, perm := range permissions {
		out.Permissions = append(out.Permissions, string(perm))
	}

	members, err := s.Store.ListMembers(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	groups, err := s.Store.ListGroups(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	for _, member := range members {
		user, err := s.Store.GetUser(ctx, member.UserID)
		if err != nil {
			return nil, err
		}
		subject, err := s.actorForUserInOrg(ctx, user, org.ID)
		if err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, s.matrixRow(subject, "user", member.UserID, member.Name, member.Email, member.RoleName, 0, permissions, scope))
	}
	machineScoped, err := s.Store.ListOrgAccessBindings(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		subject := s.actorForGroup(org.ID, group.Name, group.ID, machineScoped)
		members, err := s.Store.ListGroupMembers(ctx, group.ID)
		if err != nil {
			return nil, err
		}
		out.Rows = append(out.Rows, s.matrixRow(subject, "group", group.ID, group.Name, "", "", len(members), permissions, scope))
	}
	return out, nil
}

func (s *Service) matrixRow(subject authz.Actor, kind, id, name, email, role string, memberCount int, permissions []authz.Permission, scope authz.Scope) MatrixRow {
	row := MatrixRow{SubjectKind: kind, SubjectID: id, Name: name, Email: email, Role: role, MemberCount: memberCount}
	for _, perm := range permissions {
		if s.Engine.Authorize(subject, perm, scope, s.Now()).Allowed {
			row.Permissions = append(row.Permissions, string(perm))
		}
	}
	return row
}

// ExplainAccess evaluates one subject/permission/scope and lists the bindings.
func (s *Service) ExplainAccess(ctx context.Context, actor authz.Actor, orgSlug string, in ExplainInput) (*AccessExplanation, error) {
	org, err := s.membershipOrg(ctx, actor, orgSlug)
	if err != nil {
		return nil, err
	}
	if err := s.Authorize(actor, authz.RolesManage, authz.Scope{OrgID: org.ID}); err != nil {
		return nil, err
	}
	perm := authz.Permission(in.Permission)
	if !perm.Valid() {
		return nil, fmt.Errorf("%w: unknown permission", ErrValidation)
	}
	scope := authz.Scope{OrgID: org.ID, Environment: in.Environment, Path: in.Path}
	if scope.Environment == "" {
		scope.Environment = "*"
	}
	if scope.Path == "" {
		scope.Path = "/"
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
		scope.ProjectID = project.ID
	}

	kind := strings.ToLower(strings.TrimSpace(in.SubjectKind))
	if kind == "" {
		kind = "user"
	}
	all, err := s.Store.ListOrgAccessBindings(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	directKeys := map[string]bool{}
	groupKeys := map[string]bool{}
	var subject authz.Actor
	var subjectName string
	switch kind {
	case "user":
		user, err := s.Store.GetUser(ctx, in.SubjectID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		subject, err = s.actorForUserInOrg(ctx, user, org.ID)
		if err != nil {
			return nil, err
		}
		subjectName = user.Email
		groupIDs, err := s.Store.ListUserGroupIDs(ctx, user.ID)
		if err != nil {
			return nil, err
		}
		inGroup := make(map[string]bool, len(groupIDs))
		for _, groupID := range groupIDs {
			inGroup[groupID] = true
		}
		for _, b := range all {
			switch b.SubjectKind {
			case "user":
				if b.SubjectID == user.ID {
					directKeys[bindingKey(b.RoleName, b.ProjectID, b.EnvironmentGlob, b.PathGlob)] = true
				}
			case "group":
				if inGroup[b.SubjectID] {
					groupKeys[bindingKey(b.RoleName, b.ProjectID, b.EnvironmentGlob, b.PathGlob)] = true
				}
			}
		}
	case "group":
		group, err := s.Store.GetGroup(ctx, org.ID, in.SubjectID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, ErrNotFound
			}
			return nil, err
		}
		subject = s.actorForGroup(org.ID, group.Name, group.ID, all)
		subjectName = group.Name
		for _, b := range all {
			if b.SubjectKind == "group" && b.SubjectID == group.ID {
				groupKeys[bindingKey(b.RoleName, b.ProjectID, b.EnvironmentGlob, b.PathGlob)] = true
			}
		}
	default:
		return nil, fmt.Errorf("%w: subject_kind must be user or group", ErrValidation)
	}

	decision := s.Engine.Authorize(subject, perm, scope, s.Now())
	out := &AccessExplanation{
		Allowed: decision.Allowed, Reason: decision.Reason, Subject: subjectName,
		Scope: in, ProjectID: projectID,
	}
	for i := range subject.Bindings {
		b := &subject.Bindings[i]
		view := BindingView{
			Role: b.Role, Source: "direct", SubjectKind: kind, SubjectID: in.SubjectID,
			ProjectID: b.Scope.ProjectID, Environment: b.Scope.Environment, Path: b.Scope.Path,
			Grants: bindingAllows(*b, perm),
		}
		if !b.ExpiresAt.IsZero() {
			expiry := b.ExpiresAt
			view.ExpiresAt = &expiry
		}
		key := bindingKey(b.Role, b.Scope.ProjectID, b.Scope.Environment, b.Scope.Path)
		switch {
		case b.Scope.ProjectID == "":
			view.Source = "organization"
		case directKeys[key]:
			view.Source = "direct"
		case groupKeys[key]:
			view.Source = "group"
		default:
			view.Source = "inherited"
		}
		if decision.Binding != nil && b.Scope == decision.Binding.Scope && b.Role == decision.Binding.Role {
			matched := view
			out.Matched = &matched
		}
		out.Bindings = append(out.Bindings, view)
	}
	return out, nil
}

func (s *Service) actorForUserInOrg(ctx context.Context, user *store.User, orgID string) (authz.Actor, error) {
	full, err := s.ActorForUser(ctx, user)
	if err != nil {
		return authz.Actor{}, err
	}
	actor := authz.Actor{ID: full.ID, Kind: full.Kind, Name: full.Name, OrgID: orgID}
	for _, binding := range full.Bindings {
		if binding.Scope.OrgID == orgID {
			actor.Bindings = append(actor.Bindings, binding)
		}
	}
	return actor, nil
}

func (s *Service) actorForGroup(orgID, name, groupID string, all []store.UserBinding) authz.Actor {
	actor := authz.Actor{ID: groupID, Kind: authz.SubjectGroup, Name: name, OrgID: orgID}
	for _, b := range all {
		if b.SubjectKind != "group" || b.SubjectID != groupID {
			continue
		}
		binding := authz.Binding{
			Role:        b.RoleName,
			Permissions: parsePermissions(b.RolePermissions),
			Scope:       authz.Scope{OrgID: b.OrgID, ProjectID: b.ProjectID, Environment: b.EnvironmentGlob, Path: b.PathGlob},
		}
		if b.ExpiresAt != nil {
			binding.ExpiresAt = *b.ExpiresAt
		}
		actor.Bindings = append(actor.Bindings, binding)
	}
	return actor
}

func bindingKey(role, projectID, environment, path string) string {
	return role + "|" + projectID + "|" + environment + "|" + path
}

func bindingAllows(binding authz.Binding, perm authz.Permission) bool {
	if binding.Role != "" {
		if perms, ok := authz.BuiltinRolePermissions(binding.Role); ok {
			for _, candidate := range perms {
				if candidate == perm {
					return true
				}
			}
		}
	}
	for _, candidate := range binding.Permissions {
		if candidate == perm {
			return true
		}
	}
	return false
}
