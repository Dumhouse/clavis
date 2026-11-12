package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type sharedSetItemBody struct {
	Key       string    `json:"key"`
	Kind      string    `json:"kind"`
	Comment   string    `json:"comment,omitempty"`
	Value     string    `json:"value,omitempty"`
	Version   int       `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sharedSetBody struct {
	ID        string                `json:"id"`
	Slug      string                `json:"slug"`
	Name      string                `json:"name"`
	Items     []sharedSetItemBody   `json:"items"`
	Imports   []sharedSetImportBody `json:"imports"`
	CreatedAt time.Time             `json:"created_at"`
}

type sharedSetsOutput struct {
	Body struct {
		Sets []sharedSetBody `json:"sets"`
	}
}

type sharedSetOutput struct {
	Body sharedSetBody
}

type createSharedSetInput struct {
	Org  string `path:"org"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
	}
}

type sharedSetPath struct {
	Org  string `path:"org"`
	Slug string `path:"slug"`
}

type sharedSetItemGetInput struct {
	Org    string `path:"org"`
	Slug   string `path:"slug"`
	Reveal bool   `query:"reveal,omitempty"`
}

type sharedSetItemPath struct {
	Org  string `path:"org"`
	Slug string `path:"slug"`
	Key  string `path:"key"`
}

type sharedSetItemInput struct {
	Org  string `path:"org"`
	Slug string `path:"slug"`
	Key  string `path:"key"`
	Body struct {
		Value   string `json:"value" maxLength:"1048576"`
		Kind    string `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
		Comment string `json:"comment,omitempty" maxLength:"4096"`
	}
}

type sharedSetEnvironmentPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Slug    string `path:"slug"`
}

type sharedSetImportBody struct {
	ID          string    `json:"id"`
	SetSlug     string    `json:"set_slug"`
	SetName     string    `json:"set_name"`
	ProjectSlug string    `json:"project_slug,omitempty"`
	Environment string    `json:"environment_slug,omitempty"`
	Priority    int       `json:"priority"`
	CreatedAt   time.Time `json:"created_at"`
}

type sharedSetImportsOutput struct {
	Body struct {
		Imports []sharedSetImportBody `json:"imports"`
	}
}

func (s *Server) registerSharedSetRoutes() error {
	base := "/api/v1/orgs/{org}/sets"
	if err := s.registry.Declare(http.MethodGet, base, authz.OrgRead, "list shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-shared-sets", Method: http.MethodGet, Path: base,
		Summary: "List shared sets", Tags: []string{"sets"},
	}, func(ctx context.Context, in *orgPath) (*sharedSetsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListSharedSets(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &sharedSetsOutput{}
		out.Body.Sets = []sharedSetBody{}
		for _, view := range views {
			out.Body.Sets = append(out.Body.Sets, sharedSetFromView(view, false))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.RolesManage, "create shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-shared-set", Method: http.MethodPost, Path: base,
		Summary: "Create a shared set", Tags: []string{"sets"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSharedSetInput) (*sharedSetOutput, error) {
		actor, _ := actorFrom(ctx)
		set, err := s.svc.CreateSharedSet(ctx, actor, in.Org, in.Body.Name, in.Body.Slug, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &sharedSetOutput{Body: sharedSetFromView(service.SharedSetView{Set: set}, false)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, base+"/{slug}", authz.OrgRead, "read shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-shared-set", Method: http.MethodGet, Path: base + "/{slug}",
		Summary: "Get a shared set", Tags: []string{"sets"},
	}, func(ctx context.Context, in *sharedSetItemGetInput) (*sharedSetOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.GetSharedSet(ctx, actor, in.Org, in.Slug, in.Reveal)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &sharedSetOutput{Body: sharedSetFromView(*view, in.Reveal)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{slug}", authz.RolesManage, "delete shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-shared-set", Method: http.MethodDelete, Path: base + "/{slug}",
		Summary: "Delete a shared set", Tags: []string{"sets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sharedSetPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSharedSet(ctx, actor, in.Org, in.Slug, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPut, base+"/{slug}/items/{key}", authz.RolesManage, "set shared set items"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "set-shared-set-item", Method: http.MethodPut, Path: base + "/{slug}/items/{key}",
		Summary: "Set a shared set value", Tags: []string{"sets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sharedSetItemInput) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.SetSharedSetItem(ctx, actor, in.Org, in.Slug, in.Key, service.SecretInput{
			Value: in.Body.Value, Kind: in.Body.Kind, Comment: in.Body.Comment,
		}, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{slug}/items/{key}", authz.RolesManage, "delete shared set items"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-shared-set-item", Method: http.MethodDelete, Path: base + "/{slug}/items/{key}",
		Summary: "Delete a shared set value", Tags: []string{"sets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sharedSetItemPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSharedSetItem(ctx, actor, in.Org, in.Slug, in.Key, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	envBase := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/sets"
	if err := s.registry.Declare(http.MethodGet, envBase, authz.EnvironmentsRead, "list sets imported by an environment"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-environment-sets", Method: http.MethodGet, Path: envBase,
		Summary: "List imported sets", Tags: []string{"sets"},
	}, func(ctx context.Context, in *environmentPath) (*sharedSetImportsOutput, error) {
		actor, _ := actorFrom(ctx)
		imports, err := s.svc.ListEnvironmentSharedSets(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &sharedSetImportsOutput{}
		out.Body.Imports = []sharedSetImportBody{}
		for _, item := range imports {
			out.Body.Imports = append(out.Body.Imports, sharedSetImportBody{
				ID: item.ID, SetSlug: item.SetSlug, SetName: item.SetName,
				ProjectSlug: item.ProjectSlug, Environment: item.EnvironmentSlug,
				Priority: item.Priority, CreatedAt: item.CreatedAt,
			})
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, envBase+"/{slug}", authz.EnvironmentsManage, "import shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "import-shared-set", Method: http.MethodPost, Path: envBase + "/{slug}",
		Summary: "Import a shared set", Tags: []string{"sets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sharedSetEnvironmentPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.ImportSharedSet(ctx, actor, in.Org, in.Project, in.Env, in.Slug, 0, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, envBase+"/{slug}", authz.EnvironmentsManage, "detach shared sets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "detach-shared-set", Method: http.MethodDelete, Path: envBase + "/{slug}",
		Summary: "Detach a shared set", Tags: []string{"sets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *sharedSetEnvironmentPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DetachSharedSet(ctx, actor, in.Org, in.Project, in.Env, in.Slug, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func sharedSetFromView(view service.SharedSetView, _ bool) sharedSetBody {
	body := sharedSetBody{
		ID: view.Set.ID, Slug: view.Set.Slug, Name: view.Set.Name, CreatedAt: view.Set.CreatedAt,
		Items: []sharedSetItemBody{}, Imports: []sharedSetImportBody{},
	}
	for _, imported := range view.Imports {
		body.Imports = append(body.Imports, sharedSetImportBody{
			ID: imported.ID, SetSlug: imported.SetSlug, SetName: imported.SetName,
			ProjectSlug: imported.ProjectSlug, Environment: imported.EnvironmentSlug,
			Priority: imported.Priority, CreatedAt: imported.CreatedAt,
		})
	}
	for _, item := range view.Items {
		body.Items = append(body.Items, sharedSetItemBody{
			Key: item.Key, Kind: item.Kind, Comment: item.Comment, Value: item.Value,
			Version: item.Version, UpdatedAt: item.UpdatedAt,
		})
	}
	return body
}
