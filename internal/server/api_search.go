package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type searchInput struct {
	Org   string `path:"org"`
	Query string `query:"q" minLength:"2" maxLength:"128" doc:"Case-insensitive substring of a key name"`
}

type keyMatchBody struct {
	Key          string   `json:"key"`
	Project      string   `json:"project"`
	ProjectName  string   `json:"project_name"`
	Environments []string `json:"environments"`
}

type searchOutput struct {
	Body struct {
		Keys []keyMatchBody `json:"keys"`
	}
}

func (s *Server) registerSearchRoutes() error {
	const path = "/api/v1/orgs/{org}/search"
	if err := s.registry.Declare(http.MethodGet, path, authz.OrgRead, "search secret key names (never values)"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "search-keys", Method: http.MethodGet, Path: path,
		Summary: "Search secret key names across projects", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *searchInput) (*searchOutput, error) {
		actor, _ := actorFrom(ctx)
		matches, err := s.svc.SearchKeys(ctx, actor, in.Org, in.Query)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &searchOutput{}
		out.Body.Keys = make([]keyMatchBody, 0, len(matches))
		for _, match := range matches {
			out.Body.Keys = append(out.Body.Keys, keyMatchBody{
				Key: match.Key, Project: match.Project, ProjectName: match.ProjectName, Environments: match.Environments,
			})
		}
		return out, nil
	})
	return nil
}
