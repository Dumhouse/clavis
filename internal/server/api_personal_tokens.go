package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/internal/store"
)

type personalTokenBody struct {
	ID          string     `json:"id"`
	Prefix      string     `json:"prefix"`
	Permissions []string   `json:"permissions"`
	ProjectID   string     `json:"project_id,omitempty"`
	Environment string     `json:"environment,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

type personalTokensOutput struct {
	Body struct {
		Tokens []personalTokenBody `json:"tokens"`
	}
}

type createPersonalTokenInput struct {
	Org  string `path:"org"`
	Body struct {
		Permissions   []string `json:"permissions,omitempty" maxItems:"20"`
		ProjectSlug   string   `json:"project,omitempty" maxLength:"60"`
		Environment   string   `json:"environment,omitempty" maxLength:"60"`
		ExpiresInDays int      `json:"expires_in_days,omitempty" minimum:"1" maximum:"365"`
	}
}

type personalTokenOutput struct {
	Body struct {
		Token     string            `json:"token"`
		TokenInfo personalTokenBody `json:"token_info"`
	}
}

type personalTokenPath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

func (s *Server) registerPersonalTokenRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/tokens", authz.OrgRead, "list the caller's personal access tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-personal-tokens", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/tokens",
		Summary: "List personal access tokens", Tags: []string{"tokens"},
	}, func(ctx context.Context, in *orgPath) (*personalTokensOutput, error) {
		actor, _ := actorFrom(ctx)
		tokens, err := s.svc.ListPersonalTokens(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &personalTokensOutput{}
		for i := range tokens {
			out.Body.Tokens = append(out.Body.Tokens, personalTokenFromStore(&tokens[i]))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/tokens", authz.OrgRead, "create the caller's own personal access token"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-personal-token", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/tokens",
		Summary: "Create a personal access token", Tags: []string{"tokens"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createPersonalTokenInput) (*personalTokenOutput, error) {
		actor, _ := actorFrom(ctx)
		permissions := make([]authz.Permission, 0, len(in.Body.Permissions))
		for _, name := range in.Body.Permissions {
			permissions = append(permissions, authz.Permission(name))
		}
		result, err := s.svc.CreatePersonalToken(ctx, actor, in.Org, service.PersonalTokenInput{
			Permissions: permissions, ProjectSlug: in.Body.ProjectSlug,
			Environment: in.Body.Environment, ExpiresInDays: in.Body.ExpiresInDays,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &personalTokenOutput{}
		out.Body.Token = result.Token
		out.Body.TokenInfo = personalTokenFromStore(result.Record)
		return out, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/tokens/{id}", authz.OrgRead, "revoke the caller's own personal access token"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-personal-token", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/tokens/{id}",
		Summary: "Revoke a personal access token", Tags: []string{"tokens"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *personalTokenPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokePersonalToken(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func personalTokenFromStore(token *store.AccessToken) personalTokenBody {
	var scopes struct {
		Permissions []string `json:"permissions"`
		ProjectID   string   `json:"project_id"`
		Environment string   `json:"environment"`
	}
	_ = json.Unmarshal([]byte(token.Scopes), &scopes)
	return personalTokenBody{
		ID: token.ID, Prefix: token.Prefix, Permissions: scopes.Permissions,
		ProjectID: scopes.ProjectID, Environment: scopes.Environment,
		ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, CreatedAt: token.CreatedAt,
	}
}
