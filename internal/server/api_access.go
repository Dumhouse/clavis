package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type grantBody struct {
	ID          string     `json:"id"`
	SubjectKind string     `json:"subject_kind"`
	SubjectID   string     `json:"subject_id"`
	SubjectName string     `json:"subject_name,omitempty"`
	SubjectType string     `json:"subject_type,omitempty"`
	UserID      string     `json:"user_id,omitempty"`
	GroupID     string     `json:"group_id,omitempty"`
	Email       string     `json:"email,omitempty"`
	Name        string     `json:"name,omitempty"`
	Role        string     `json:"role"`
	Environment string     `json:"environment"`
	Path        string     `json:"path"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type grantsOutput struct {
	Body struct {
		Grants []grantBody `json:"grants"`
	}
}

type grantOutput struct {
	Body grantBody
}

type createGrantInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		SubjectKind   string `json:"subject_kind,omitempty" enum:"user,group" default:"user"`
		SubjectID     string `json:"subject_id,omitempty"`
		UserID        string `json:"user_id,omitempty"`
		Role          string `json:"role,omitempty" default:"Developer"`
		Environment   string `json:"environment,omitempty" maxLength:"60"`
		Path          string `json:"path,omitempty" maxLength:"512"`
		ExpiresInDays int    `json:"expires_in_days,omitempty" minimum:"0" maximum:"365"`
	}
}

type grantPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

const projectAccessBase = "/api/v1/orgs/{org}/projects/{project}/access"

func (s *Server) registerAccessRoutes() error {
	if err := s.registry.Declare(http.MethodGet, projectAccessBase, authz.RolesManage, "list project-scoped grants"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-project-access", Method: http.MethodGet, Path: projectAccessBase,
		Summary: "List project access grants", Tags: []string{"access"},
	}, func(ctx context.Context, in *projectPath) (*grantsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListProjectAccess(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &grantsOutput{}
		for _, view := range views {
			out.Body.Grants = append(out.Body.Grants, grantFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, projectAccessBase, authz.RolesManage, "grant project-scoped roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "grant-project-access", Method: http.MethodPost, Path: projectAccessBase,
		Summary: "Grant project access", Tags: []string{"access"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createGrantInput) (*grantOutput, error) {
		actor, _ := actorFrom(ctx)
		binding, err := s.svc.GrantProjectAccess(ctx, actor, in.Org, in.Project, service.ProjectGrant{
			SubjectKind: in.Body.SubjectKind, SubjectID: in.Body.SubjectID, UserID: in.Body.UserID,
			Role: in.Body.Role, Environment: in.Body.Environment,
			Path: in.Body.Path, ExpiresInDays: in.Body.ExpiresInDays,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		view := service.ProjectGrantView{Binding: binding}
		switch binding.SubjectKind {
		case "user":
			if user, err := s.store.GetUser(ctx, binding.SubjectID); err == nil {
				view.UserEmail = user.Email
				view.UserName = user.Name
				view.SubjectName = user.Name
			}
		case "group":
			if group, err := s.store.GetGroup(ctx, binding.OrgID, binding.SubjectID); err == nil {
				view.SubjectName = group.Name
			}
		}
		return &grantOutput{Body: grantFromView(view)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, projectAccessBase+"/{id}", authz.RolesManage, "revoke project-scoped grants"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-project-access", Method: http.MethodDelete, Path: projectAccessBase + "/{id}",
		Summary: "Revoke project access", Tags: []string{"access"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *grantPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeProjectAccess(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func grantFromView(view service.ProjectGrantView) grantBody {
	body := grantBody{
		ID: view.Binding.ID, SubjectKind: view.Binding.SubjectKind, SubjectID: view.Binding.SubjectID,
		SubjectName: view.SubjectName, Role: view.Binding.RoleName,
		Environment: view.Binding.EnvironmentGlob, Path: view.Binding.PathGlob,
		ExpiresAt: view.Binding.ExpiresAt, CreatedAt: view.Binding.CreatedAt,
	}
	switch view.Binding.SubjectKind {
	case "group":
		body.GroupID = view.Binding.SubjectID
		body.SubjectType = "group"
	default:
		body.UserID = view.Binding.SubjectID
		body.SubjectType = "user"
		body.Email = view.UserEmail
		body.Name = view.UserName
	}
	return body
}
