package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type groupMemberBody struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Name   string `json:"name"`
}

type groupBody struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Members []groupMemberBody `json:"members"`
}

type groupsOutput struct {
	Body struct {
		Groups []groupBody `json:"groups"`
	}
}

type groupOutput struct {
	Body groupBody
}

type createGroupInput struct {
	Org  string `path:"org"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}
}

type groupPath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

type renameGroupInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
	}
}

type groupMemberPath struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	User string `path:"user"`
}

type groupMemberInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		UserID string `json:"user_id" minLength:"1"`
	}
}

func (s *Server) registerGroupRoutes() error {
	base := "/api/v1/orgs/{org}/groups"
	if err := s.registry.Declare(http.MethodGet, base, authz.MembersManage, "list groups"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-groups", Method: http.MethodGet, Path: base,
		Summary: "List groups", Tags: []string{"groups"},
	}, func(ctx context.Context, in *orgPath) (*groupsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListGroups(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &groupsOutput{}
		for _, view := range views {
			out.Body.Groups = append(out.Body.Groups, groupFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.MembersManage, "create groups"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-group", Method: http.MethodPost, Path: base,
		Summary: "Create a group", Tags: []string{"groups"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createGroupInput) (*groupOutput, error) {
		actor, _ := actorFrom(ctx)
		group, err := s.svc.CreateGroup(ctx, actor, in.Org, in.Body.Name, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &groupOutput{Body: groupFromView(service.GroupView{Group: group})}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, base+"/{id}", authz.MembersManage, "rename groups"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "rename-group", Method: http.MethodPatch, Path: base + "/{id}",
		Summary: "Rename a group", Tags: []string{"groups"},
	}, func(ctx context.Context, in *renameGroupInput) (*groupOutput, error) {
		actor, _ := actorFrom(ctx)
		group, err := s.svc.RenameGroup(ctx, actor, in.Org, in.ID, in.Body.Name, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &groupOutput{Body: groupFromView(service.GroupView{Group: group})}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.MembersManage, "delete groups"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-group", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a group", Tags: []string{"groups"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *groupPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteGroup(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPost, base+"/{id}/members", authz.MembersManage, "add group members"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "add-group-member", Method: http.MethodPost, Path: base + "/{id}/members",
		Summary: "Add a group member", Tags: []string{"groups"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *groupMemberInput) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.AddGroupMember(ctx, actor, in.Org, in.ID, in.Body.UserID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}/members/{user}", authz.MembersManage, "remove group members"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "remove-group-member", Method: http.MethodDelete, Path: base + "/{id}/members/{user}",
		Summary: "Remove a group member", Tags: []string{"groups"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *groupMemberPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RemoveGroupMember(ctx, actor, in.Org, in.ID, in.User, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func groupFromView(view service.GroupView) groupBody {
	body := groupBody{ID: view.Group.ID, Name: view.Group.Name}
	for _, member := range view.Members {
		body.Members = append(body.Members, groupMemberBody{
			UserID: member.UserID, Email: member.Email, Name: member.Name,
		})
	}
	return body
}
