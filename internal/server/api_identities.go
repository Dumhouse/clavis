package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type identityTokenBody struct {
	ID         string     `json:"id"`
	Prefix     string     `json:"prefix"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	LastUsedIP string     `json:"last_used_ip,omitempty"`
}

type identityBody struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Type         string              `json:"type"`
	Role         string              `json:"role"`
	ProjectID    string              `json:"project_id,omitempty"`
	Environment  string              `json:"environment,omitempty"`
	OIDCIssuer   string              `json:"oidc_issuer,omitempty"`
	OIDCAudience string              `json:"oidc_audience,omitempty"`
	OIDCClaims   map[string]string   `json:"oidc_claims,omitempty"`
	AWSAccount   string              `json:"aws_account,omitempty"`
	AWSARN       string              `json:"aws_arn,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	Tokens       []identityTokenBody `json:"tokens"`
}

type identitiesOutput struct {
	Body struct {
		Identities []identityBody `json:"identities"`
	}
}

type createIdentityInput struct {
	Org  string `path:"org"`
	Body struct {
		Name          string `json:"name" minLength:"1" maxLength:"100"`
		Type          string `json:"type,omitempty" enum:"machine,agent" default:"machine"`
		Role          string `json:"role,omitempty" default:"Developer"`
		ProjectSlug   string `json:"project,omitempty" maxLength:"60"`
		Environment   string `json:"environment,omitempty" maxLength:"60" doc:"Environment glob, e.g. dev or *"`
		ExpiresInDays int    `json:"expires_in_days,omitempty" minimum:"1" maximum:"365" default:"90"`
		OIDC          *struct {
			Issuer   string            `json:"issuer" minLength:"1" maxLength:"500"`
			Audience string            `json:"audience" minLength:"1" maxLength:"200"`
			Claims   map[string]string `json:"claims,omitempty"`
		} `json:"oidc,omitempty"`
		AWS *struct {
			Account       string `json:"account,omitempty" maxLength:"12"`
			ARNPattern    string `json:"arn_pattern,omitempty" maxLength:"500"`
			UserIDPattern string `json:"user_id_pattern,omitempty" maxLength:"500"`
		} `json:"aws,omitempty"`
	}
}

type identityOutput struct {
	Body struct {
		Identity identityBody `json:"identity"`
		Token    string       `json:"token"`
	}
}

type identityPath struct {
	Org    string `path:"org"`
	ID     string `path:"id"`
	Reauth string `header:"X-Clavis-Reauth"`
}

func (s *Server) registerIdentityRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/identities", authz.TokensManage, "list machine and agent identities"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-identities", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/identities",
		Summary: "List identities", Tags: []string{"identities"},
	}, func(ctx context.Context, in *orgPath) (*identitiesOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListIdentities(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &identitiesOutput{}
		for _, view := range views {
			out.Body.Identities = append(out.Body.Identities, identityFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/identities", authz.TokensManage, "create identities and their tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-identity", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/identities",
		Summary: "Create an identity and token", Tags: []string{"identities"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createIdentityInput) (*identityOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.CreateIdentity(ctx, actor, in.Org, service.IdentityInput{
			Name:          in.Body.Name,
			Type:          in.Body.Type,
			Role:          in.Body.Role,
			ProjectSlug:   in.Body.ProjectSlug,
			Environment:   in.Body.Environment,
			ExpiresInDays: in.Body.ExpiresInDays,
			OIDC:          oidcTrustInput(in.Body.OIDC),
			AWS:           awsTrustInput(in.Body.AWS),
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &identityOutput{}
		out.Body.Token = result.Token
		out.Body.Identity = identityFromResult(result)
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/identities/{id}/tokens", authz.TokensManage, "rotate identity tokens"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "rotate-identity-token", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/identities/{id}/tokens",
		Summary: "Rotate an identity token", Tags: []string{"identities"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *identityPath) (*identityOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		result, err := s.svc.RotateIdentityToken(ctx, actor, in.Org, in.ID, 90, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &identityOutput{}
		out.Body.Token = result.Token
		out.Body.Identity = identityFromResult(result)
		return out, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/identities/{id}", authz.TokensManage, "revoke identities"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-identity", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/identities/{id}",
		Summary: "Revoke an identity", Tags: []string{"identities"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *identityPath) (*emptyOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeIdentity(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/oidc", "workloads exchange a signed OIDC token for a short-lived Clavis token"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "exchange-oidc", Method: http.MethodPost, Path: "/api/v1/auth/oidc",
		Summary: "Exchange a workload OIDC token", Tags: []string{"auth", "identities"},
	}, func(ctx context.Context, in *oidcExchangeInput) (*oidcExchangeOutput, error) {
		result, err := s.svc.ExchangeOIDC(ctx, in.Body.Org, in.Body.Identity, in.Body.Token, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &oidcExchangeOutput{}
		out.Body.Token = result.Token
		out.Body.Identity = result.Identity.Name
		out.Body.ExpiresAt = result.Record.ExpiresAt
		return out, nil
	})
	if err := s.registry.DeclarePublic(http.MethodPost, "/api/v1/auth/aws", "workloads replay a SigV4-signed GetCallerIdentity request for a short-lived token"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "exchange-aws", Method: http.MethodPost, Path: "/api/v1/auth/aws",
		Summary: "Exchange an AWS signed identity", Tags: []string{"auth", "identities"},
	}, func(ctx context.Context, in *awsExchangeInput) (*oidcExchangeOutput, error) {
		result, err := s.svc.ExchangeAWS(ctx, in.Body.Org, in.Body.Identity, service.AWSRequest{
			Authorization: in.Body.Authorization, AMZDate: in.Body.AMZDate,
			SecurityToken: in.Body.SecurityToken, ContentType: in.Body.ContentType,
			ContentSHA256: in.Body.ContentSHA256, Host: in.Body.Host,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &oidcExchangeOutput{}
		out.Body.Token = result.Token
		out.Body.Identity = result.Identity.Name
		out.Body.ExpiresAt = result.Record.ExpiresAt
		return out, nil
	})
	return nil
}

func identityFromView(view service.IdentityView) identityBody {
	body := identityBody{
		ID: view.Identity.ID, Name: view.Identity.Name, Type: view.Identity.Type,
		Role: view.Scope.Role, ProjectID: view.Scope.ProjectID, Environment: view.Scope.Environment,
		CreatedAt: view.Identity.CreatedAt,
	}
	if view.OIDC != nil {
		body.OIDCIssuer = view.OIDC.Issuer
		body.OIDCAudience = view.OIDC.Audience
		body.OIDCClaims = view.OIDC.Claims
	}
	if view.AWS != nil {
		body.AWSAccount = view.AWS.Account
		body.AWSARN = view.AWS.ARNPattern
	}
	for _, token := range view.Tokens {
		body.Tokens = append(body.Tokens, identityTokenBody{
			ID: token.ID, Prefix: token.Prefix, CreatedAt: token.CreatedAt,
			ExpiresAt: token.ExpiresAt, LastUsedAt: token.LastUsedAt, LastUsedIP: token.LastUsedIP,
		})
	}
	return body
}

func identityFromResult(result *service.IdentityResult) identityBody {
	return identityBody{
		ID: result.Identity.ID, Name: result.Identity.Name, Type: result.Identity.Type,
		CreatedAt: result.Identity.CreatedAt,
		Tokens: []identityTokenBody{{
			ID: result.Record.ID, Prefix: result.Record.Prefix, CreatedAt: result.Record.CreatedAt,
			ExpiresAt: result.Record.ExpiresAt,
		}},
	}
}

func oidcTrustInput(in *struct {
	Issuer   string            `json:"issuer" minLength:"1" maxLength:"500"`
	Audience string            `json:"audience" minLength:"1" maxLength:"200"`
	Claims   map[string]string `json:"claims,omitempty"`
}) *service.OIDCTrust {
	if in == nil {
		return nil
	}
	return &service.OIDCTrust{Issuer: in.Issuer, Audience: in.Audience, Claims: in.Claims}
}

func awsTrustInput(in *struct {
	Account       string `json:"account,omitempty" maxLength:"12"`
	ARNPattern    string `json:"arn_pattern,omitempty" maxLength:"500"`
	UserIDPattern string `json:"user_id_pattern,omitempty" maxLength:"500"`
}) *service.AWSTrust {
	if in == nil {
		return nil
	}
	return &service.AWSTrust{Account: in.Account, ARNPattern: in.ARNPattern, UserIDPattern: in.UserIDPattern}
}

type oidcExchangeInput struct {
	Body struct {
		Org      string `json:"org" minLength:"1" maxLength:"60"`
		Identity string `json:"identity" minLength:"1" maxLength:"100"`
		Token    string `json:"token" minLength:"1" maxLength:"16384"`
	}
}

type oidcExchangeOutput struct {
	Body struct {
		Token     string     `json:"token"`
		Identity  string     `json:"identity"`
		ExpiresAt *time.Time `json:"expires_at,omitempty"`
	}
}

type awsExchangeInput struct {
	Body struct {
		Org           string `json:"org" minLength:"1" maxLength:"60"`
		Identity      string `json:"identity" minLength:"1" maxLength:"100"`
		Authorization string `json:"authorization" minLength:"1" maxLength:"4096"`
		AMZDate       string `json:"amz_date" minLength:"1" maxLength:"100"`
		SecurityToken string `json:"security_token,omitempty" maxLength:"4096"`
		ContentType   string `json:"content_type,omitempty" maxLength:"200"`
		ContentSHA256 string `json:"content_sha256,omitempty" maxLength:"200"`
		Host          string `json:"host,omitempty" maxLength:"200"`
	}
}
