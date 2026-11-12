package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type folderBody struct {
	ID        string    `json:"id"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

type foldersOutput struct {
	Body struct {
		Folders []folderBody `json:"folders"`
	}
}

type createFolderInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Path string `json:"path" minLength:"1" maxLength:"512"`
	}
}

type folderPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	ID      string `path:"id"`
}

func folderBase(org, project, env string) string {
	return "/api/v1/orgs/" + org + "/projects/" + project + "/environments/" + env + "/folders"
}

func (s *Server) registerFolderRoutes() error {
	if err := s.registry.Declare(http.MethodGet, folderBase("{org}", "{project}", "{env}"), authz.SecretsReadNames, "list folders"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-folders", Method: http.MethodGet, Path: folderBase("{org}", "{project}", "{env}"),
		Summary: "List folders", Tags: []string{"folders"},
	}, func(ctx context.Context, in *secretsEnvPath) (*foldersOutput, error) {
		actor, _ := actorFrom(ctx)
		folders, err := s.svc.ListFolders(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &foldersOutput{}
		for _, folder := range folders {
			out.Body.Folders = append(out.Body.Folders, folderBody{ID: folder.ID, Path: folder.Path, CreatedAt: folder.CreatedAt})
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, folderBase("{org}", "{project}", "{env}"), authz.EnvironmentsManage, "create folders"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-folder", Method: http.MethodPost, Path: folderBase("{org}", "{project}", "{env}"),
		Summary: "Create a folder", Tags: []string{"folders"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createFolderInput) (*folderOutput, error) {
		actor, _ := actorFrom(ctx)
		folder, err := s.svc.CreateFolder(ctx, actor, in.Org, in.Project, in.Env, in.Body.Path, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &folderOutput{Body: folderBody{ID: folder.ID, Path: folder.Path, CreatedAt: folder.CreatedAt}}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, folderBase("{org}", "{project}", "{env}")+"/{id}", authz.EnvironmentsManage, "delete folders"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-folder", Method: http.MethodDelete, Path: folderBase("{org}", "{project}", "{env}") + "/{id}",
		Summary: "Delete a folder", Tags: []string{"folders"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *folderPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteFolder(ctx, actor, in.Org, in.Project, in.Env, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

type folderOutput struct {
	Body folderBody
}

type secretsEnvPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
}
