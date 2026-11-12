package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type branchEnvironmentInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Branch string `json:"branch" minLength:"1" maxLength:"255"`
	}
}

func (s *Server) registerBranchRoutes() error {
	path := "/api/v1/orgs/{org}/projects/{project}/environments:branch"
	if err := s.registry.Declare(http.MethodPost, path, authz.EnvironmentsManage, "create or refresh a branch environment"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "ensure-branch-environment", Method: http.MethodPost, Path: path,
		Summary: "Create or refresh a branch environment", Tags: []string{"environments"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *branchEnvironmentInput) (*environmentOutput, error) {
		actor, _ := actorFrom(ctx)
		env, err := s.svc.EnsureBranchEnvironment(ctx, actor, in.Org, in.Project, in.Body.Branch, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &environmentOutput{Body: envFromStore(env)}, nil
	})
	return nil
}
