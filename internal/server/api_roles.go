package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type roleBody struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
	Builtin     bool     `json:"builtin"`
}

type rolesOutput struct {
	Body struct {
		Roles []roleBody `json:"roles"`
	}
}

type roleOutput struct {
	Body roleBody
}

type createRoleInput struct {
	Org  string `path:"org"`
	Body struct {
		Name        string   `json:"name" minLength:"1" maxLength:"60"`
		Permissions []string `json:"permissions" minItems:"1" maxItems:"30"`
	}
}

type updateRoleInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		Name        string   `json:"name" minLength:"1" maxLength:"60"`
		Permissions []string `json:"permissions" minItems:"1" maxItems:"30"`
	}
}

type rolePath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

func (s *Server) registerRoleRoutes() error {
	base := "/api/v1/orgs/{org}/roles"
	if err := s.registry.Declare(http.MethodGet, base, authz.RolesManage, "list roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-roles", Method: http.MethodGet, Path: base,
		Summary: "List roles", Tags: []string{"roles"},
	}, func(ctx context.Context, in *orgPath) (*rolesOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListRoles(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &rolesOutput{}
		for _, view := range views {
			out.Body.Roles = append(out.Body.Roles, roleFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.RolesManage, "create custom roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-role", Method: http.MethodPost, Path: base,
		Summary: "Create a custom role", Tags: []string{"roles"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createRoleInput) (*roleOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.CreateRole(ctx, actor, in.Org, roleInput(in.Body.Name, in.Body.Permissions), metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &roleOutput{Body: roleFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, base+"/{id}", authz.RolesManage, "edit custom roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-role", Method: http.MethodPatch, Path: base + "/{id}",
		Summary: "Edit a custom role", Tags: []string{"roles"},
	}, func(ctx context.Context, in *updateRoleInput) (*roleOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.UpdateRole(ctx, actor, in.Org, in.ID, roleInput(in.Body.Name, in.Body.Permissions), metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &roleOutput{Body: roleFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.RolesManage, "delete custom roles"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-role", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a custom role", Tags: []string{"roles"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *rolePath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteRole(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func roleInput(name string, permissions []string) service.RoleInput {
	out := service.RoleInput{Name: name, Permissions: make([]authz.Permission, 0, len(permissions))}
	for _, perm := range permissions {
		out.Permissions = append(out.Permissions, authz.Permission(perm))
	}
	return out
}

func roleFromView(view service.RoleView) roleBody {
	body := roleBody{ID: view.Role.ID, Name: view.Role.Name, Builtin: view.Role.Builtin}
	for _, perm := range view.Permissions {
		body.Permissions = append(body.Permissions, string(perm))
	}
	return body
}
