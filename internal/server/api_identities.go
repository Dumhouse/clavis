package server

import (
	"context"
	"encoding/json"
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
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	Type            string              `json:"type"`
	Role            string              `json:"role"`
	ProjectID       string              `json:"project_id,omitempty"`
	Environment     string              `json:"environment,omitempty"`
	OIDCIssuer      string              `json:"oidc_issuer,omitempty"`
	OIDCAudience    string              `json:"oidc_audience,omitempty"`
	OIDCClaims      map[string]string   `json:"oidc_claims,omitempty"`
	AWSAccount      string              `json:"aws_account,omitempty"`
	AWSARN          string              `json:"aws_arn,omitempty"`
	AllowedCommands []string            `json:"allowed_commands,omitempty"`
	CreatedAt       time.Time           `json:"created_at"`
	Tokens          []identityTokenBody `json:"tokens"`
	ActiveSessions  int                 `json:"active_sessions" doc:"Unexpired agent sessions started with clavis mcp --agent"`
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
		Role          string `json:"role,omitempty"`
		ProjectSlug   string `json:"project,omitempty" maxLength:"60"`
		Environment   string `json:"environment,omitempty" maxLength:"60" doc:"Environment glob, e.g. dev or *"`
		ExpiresInDays int    `json:"expires_in_days,omitempty" minimum:"1" maximum:"365"`
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

type agentSessionOutput struct {
	Body struct {
		Token       string    `json:"token" doc:"Agent token; keep it in memory only"`
		IdentityID  string    `json:"identity_id"`
		Identity    string    `json:"identity"`
		Environment string    `json:"environment"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
}

type agentCommandsInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		Commands []string `json:"commands" maxItems:"100"`
	}
}

type agentCommandCheckInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		Command string `json:"command" minLength:"1" maxLength:"2048"`
	}
}

type agentAllowlistBody struct {
	IdentityID string   `json:"identity_id"`
	Name       string   `json:"name"`
	Commands   []string `json:"commands"`
}

type agentAllowlistOutput struct {
	Body agentAllowlistBody
}

type agentCommandCheckOutput struct {
	Body struct {
		Allowed bool   `json:"allowed"`
		Command string `json:"command"`
	}
}

type agentSecretsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Command string `query:"command" minLength:"1" maxLength:"2048"`
}

type secretRequestBody struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Key    string `json:"key" minLength:"1" maxLength:"256"`
		Reason string `json:"reason,omitempty" maxLength:"1024"`
	}
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

	// The service authorizes this route: the caller must be a person who holds
	// the agent's access themselves.
	sessionsPath := "/api/v1/orgs/{org}/identities/{id}/sessions"
	if err := s.registry.DeclareAuthenticated(http.MethodPost, sessionsPath, "start a short-lived agent session for clavis mcp"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "start-agent-session", Method: http.MethodPost, Path: sessionsPath,
		Summary:     "Start a delegated agent session",
		Description: "Returns a one-hour agent token tied to the caller's own session or personal access token. `{id}` may be the identity ID or name.",
		Tags:        []string{"identities"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *identityPath) (*agentSessionOutput, error) {
		actor, _ := actorFrom(ctx)
		parent := service.AgentSessionParent{}
		if principal, ok := principalFrom(ctx); ok && principal.Session != nil {
			parent = service.AgentSessionParent{Kind: service.ParentSession, ID: principal.Session.ID}
		} else if actor.Kind == authz.SubjectUser && len(actor.Bindings) == 1 {
			// Personal access tokens resolve to a single binding carrying the token ID.
			parent = service.AgentSessionParent{Kind: service.ParentPAT, ID: actor.Bindings[0].ID}
		}
		session, err := s.svc.StartAgentSession(ctx, actor, parent, in.Org, in.ID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &agentSessionOutput{}
		out.Body.Token = session.Token
		out.Body.IdentityID = session.IdentityID
		out.Body.Identity = session.Name
		out.Body.Environment = session.Environment
		out.Body.ExpiresAt = session.ExpiresAt
		return out, nil
	})

	commandsPath := "/api/v1/orgs/{org}/identities/{id}/commands"
	if err := s.registry.Declare(http.MethodGet, commandsPath, authz.TokensManage, "read an agent command allowlist"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-agent-commands", Method: http.MethodGet, Path: commandsPath,
		Summary: "Read an agent command allowlist", Tags: []string{"identities"},
	}, func(ctx context.Context, in *identityPath) (*agentAllowlistOutput, error) {
		actor, _ := actorFrom(ctx)
		allowlist, err := s.svc.GetAgentAllowlist(ctx, actor, in.Org, in.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &agentAllowlistOutput{Body: agentAllowlistBody{
			IdentityID: allowlist.IdentityID, Name: allowlist.Name, Commands: allowlist.Commands,
		}}, nil
	})
	if err := s.registry.Declare(http.MethodPut, commandsPath, authz.TokensManage, "replace an agent command allowlist"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "set-agent-commands", Method: http.MethodPut, Path: commandsPath,
		Summary: "Replace an agent command allowlist", Tags: []string{"identities"},
	}, func(ctx context.Context, in *agentCommandsInput) (*agentAllowlistOutput, error) {
		actor, _ := actorFrom(ctx)
		allowlist, err := s.svc.SetAgentAllowlist(ctx, actor, in.Org, in.ID, in.Body.Commands, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &agentAllowlistOutput{Body: agentAllowlistBody{
			IdentityID: allowlist.IdentityID, Name: allowlist.Name, Commands: allowlist.Commands,
		}}, nil
	})

	checkPath := commandsPath + ":check"
	if err := s.registry.Declare(http.MethodPost, checkPath, authz.SecretsUse, "check whether an agent command is allowed"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "check-agent-command", Method: http.MethodPost, Path: checkPath,
		Summary: "Check an agent command against its allowlist", Tags: []string{"identities"},
	}, func(ctx context.Context, in *agentCommandCheckInput) (*agentCommandCheckOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.CheckAgentCommand(ctx, actor, in.Org, in.ID, in.Body.Command, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &agentCommandCheckOutput{}
		out.Body.Allowed = true
		out.Body.Command = in.Body.Command
		return out, nil
	})

	agentSecretsPath := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:agent" //nolint:gosec // G101: API route path, not a credential
	if err := s.registry.Declare(http.MethodGet, agentSecretsPath, authz.SecretsUse, "resolve secrets for an allowed agent command"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "agent-secrets", Method: http.MethodGet, Path: agentSecretsPath,
		Summary: "Resolve secrets for an allowed agent command", Tags: []string{"secrets", "identities"},
	}, func(ctx context.Context, in *agentSecretsInput) (*secretsOutput, error) {
		actor, _ := actorFrom(ctx)
		secrets, etag, err := s.svc.ResolveAgentSecrets(ctx, actor, in.Org, in.Project, in.Env, in.Command, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &secretsOutput{ETag: etag}
		for i := range secrets {
			out.Body.Secrets = append(out.Body.Secrets, secretFromService(&secrets[i]))
		}
		return out, nil
	})

	requestPath := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secret-requests"
	if err := s.registry.Declare(http.MethodPost, requestPath, authz.SecretsUse, "ask a human to add a missing secret"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "request-secret", Method: http.MethodPost, Path: requestPath,
		Summary: "Ask a human to add a missing secret", Tags: []string{"secrets", "identities"}, DefaultStatus: http.StatusAccepted,
	}, func(ctx context.Context, in *secretRequestBody) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RequestSecret(ctx, actor, in.Org, in.Project, in.Env, in.Body.Key, in.Body.Reason, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func identityFromView(view service.IdentityView) identityBody {
	body := identityBody{
		ActiveSessions: view.ActiveSessions,
		ID:             view.Identity.ID, Name: view.Identity.Name, Type: view.Identity.Type,
		Role: view.Scope.Role, ProjectID: view.Scope.ProjectID, Environment: view.Scope.Environment,
		CreatedAt: view.Identity.CreatedAt,
	}
	if view.Identity.AllowedCommands != "" {
		_ = json.Unmarshal([]byte(view.Identity.AllowedCommands), &body.AllowedCommands)
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
		Role: result.Scope.Role, ProjectID: result.Scope.ProjectID,
		Environment: result.Scope.Environment, CreatedAt: result.Identity.CreatedAt,
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
