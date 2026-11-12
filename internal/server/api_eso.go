package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type esoSecretsOutput struct {
	Body map[string]string
}

// registerESORoutes exposes a flat JSON map for the External Secrets Operator
// webhook provider.
func (s *Server) registerESORoutes() error {
	path := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:eso"
	if err := s.registry.Declare(http.MethodGet, path, authz.SecretsReadValues, "External Secrets Operator webhook provider"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "eso-secrets", Method: http.MethodGet, Path: path,
		Summary: "Resolved secrets for External Secrets Operator", Tags: []string{"integrations"},
	}, func(ctx context.Context, in *environmentPath) (*esoSecretsOutput, error) {
		actor, _ := actorFrom(ctx)
		secrets, _, err := s.svc.ResolveSecrets(ctx, actor, in.Org, in.Project, in.Env, "api", metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &esoSecretsOutput{Body: make(map[string]string, len(secrets))}
		for i := range secrets {
			out.Body[secrets[i].Key] = secrets[i].Value
		}
		return out, nil
	})
	return nil
}
