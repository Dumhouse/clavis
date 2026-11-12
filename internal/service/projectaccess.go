package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ProjectGrant creates a project-scoped role binding for a user.
type ProjectGrant struct {
	SubjectKind   string
	SubjectID     string
	UserID        string
	Role          string
	Environment   string
	Path          string
	ExpiresInDays int
}

// ProjectGrantView is a binding enriched for display.
type ProjectGrantView struct {
	Binding     *store.RoleBinding
	UserEmail   string
	UserName    string
	SubjectName string
}

// GrantProjectAccess adds a scoped grant.
func (s *Service) GrantProjectAccess(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, in ProjectGrant, meta AuditMeta) (*store.RoleBinding, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.RolesManage)
	if err != nil {
		return nil, err
	}
	kind := in.SubjectKind
	if kind == "" {
		kind = "user"
	}
	subjectID := in.SubjectID
	if subjectID == "" {
		subjectID = in.UserID
	}
	if subjectID == "" {
		return nil, fmt.Errorf("%w: subject is required", ErrValidation)
	}
	switch kind {
	case "user":
		if _, err := s.Store.GetMembership(ctx, org.ID, subjectID); err != nil {
			return nil, fmt.Errorf("%w: user is not a member of this organization", ErrValidation)
		}
	case "group":
		if _, err := s.Store.GetGroup(ctx, org.ID, subjectID); err != nil {
			return nil, fmt.Errorf("%w: group does not exist", ErrValidation)
		}
	default:
		return nil, fmt.Errorf("%w: subject_kind must be user or group", ErrValidation)
	}
	if in.Role == "" {
		in.Role = authz.RoleDeveloper
	}
	scope := authz.Scope{OrgID: org.ID, ProjectID: project.ID}
	if !s.CanGrantRole(ctx, actor, org.ID, in.Role, scope) {
		return nil, fmt.Errorf("%w: you cannot grant a role with more permissions than you hold", ErrForbidden)
	}
	role, err := s.Store.GetRoleByName(ctx, org.ID, in.Role)
	if err != nil {
		return nil, fmt.Errorf("%w: role must be Owner, Admin, Developer, or Viewer", ErrValidation)
	}
	environment := strings.TrimSpace(in.Environment)
	if environment == "" {
		environment = "*"
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		path = "/**"
	} else {
		path = authz.NormalizePath(path)
	}
	var expiresAt *time.Time
	if in.ExpiresInDays > 0 {
		days := in.ExpiresInDays
		if days > 365 {
			days = 365
		}
		expiry := s.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
		expiresAt = &expiry
	}
	binding := &store.RoleBinding{
		ID:              uuid.NewString(),
		OrgID:           org.ID,
		SubjectKind:     kind,
		SubjectID:       subjectID,
		RoleID:          role.ID,
		RoleName:        role.Name,
		ProjectID:       project.ID,
		EnvironmentGlob: environment,
		PathGlob:        path,
		ExpiresAt:       expiresAt,
	}
	if err := s.Store.CreateRoleBinding(ctx, binding); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.grant",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Environment: environment, Path: path, Reason: in.Role}, meta); err != nil {
		s.Logger.Error("audit access.grant failed", "binding_id", binding.ID, "error", err)
	}
	return binding, nil
}

// ListProjectAccess returns project-scoped grants.
func (s *Service) ListProjectAccess(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) ([]ProjectGrantView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.RolesManage)
	if err != nil {
		return nil, err
	}
	bindings, err := s.Store.ListProjectBindings(ctx, org.ID, project.ID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectGrantView, 0, len(bindings))
	for i := range bindings {
		view := ProjectGrantView{Binding: &bindings[i]}
		switch bindings[i].SubjectKind {
		case string(authz.SubjectUser):
			if user, err := s.Store.GetUser(ctx, bindings[i].SubjectID); err == nil {
				view.UserEmail = user.Email
				view.UserName = user.Name
				view.SubjectName = user.Name
			}
		case "group":
			if group, err := s.Store.GetGroup(ctx, org.ID, bindings[i].SubjectID); err == nil {
				view.SubjectName = group.Name
			}
		}
		out = append(out, view)
	}
	return out, nil
}

// RevokeProjectAccess removes a grant.
func (s *Service) RevokeProjectAccess(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, bindingID string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.RolesManage)
	if err != nil {
		return err
	}
	binding, err := s.Store.GetRoleBinding(ctx, org.ID, bindingID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if binding.ProjectID != project.ID {
		return ErrNotFound
	}
	if err := s.Store.DeleteRoleBinding(ctx, org.ID, bindingID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "access.revoke",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Reason: binding.RoleName}, meta); err != nil {
		s.Logger.Error("audit access.revoke failed", "binding_id", bindingID, "error", err)
	}
	return nil
}
