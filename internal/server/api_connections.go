package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type connectionUsageBody struct {
	SyncID      string `json:"sync_id"`
	Project     string `json:"project"`
	Environment string `json:"environment"`
}

type connectionBody struct {
	ID         string     `json:"id"`
	Provider   string     `json:"provider"`
	Name       string     `json:"name"`
	BaseURL    string     `json:"base_url"`
	Auth       string     `json:"auth" enum:"token,github_app,aws_keys,service_account" doc:"Kind of stored credential; the credential itself is never returned"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	SyncCount  int        `json:"sync_count"`
	// Syncs is omitted for callers without org-wide syncs:manage.
	Syncs []connectionUsageBody `json:"syncs,omitempty"`
}

type listConnectionsInput struct {
	Org      string `path:"org"`
	Provider string `query:"provider" doc:"Only connections for this provider"`
}

type connectionsOutput struct {
	Body struct {
		Connections []connectionBody `json:"connections"`
	}
}

type connectionOutput struct {
	Body connectionBody
}

type createConnectionInput struct {
	Org  string `path:"org"`
	Body struct {
		Provider           string `json:"provider" enum:"aws,cloudflare,forgejo,gcp,github,gitlab,netlify,vault,vercel"`
		Name               string `json:"name" minLength:"1" maxLength:"100"`
		BaseURL            string `json:"base_url,omitempty" maxLength:"500" doc:"Provider API or instance URL; defaults to the public service"`
		Token              string `json:"token,omitempty" maxLength:"1000"`
		GitHubAppID        string `json:"github_app_id,omitempty" maxLength:"100"`
		GitHubInstallation string `json:"github_installation_id,omitempty" maxLength:"100"`
		GitHubPrivateKey   string `json:"github_private_key,omitempty" maxLength:"20000"`
		AWSAccessKey       string `json:"aws_access_key,omitempty" maxLength:"200"`
		AWSSecretKey       string `json:"aws_secret_key,omitempty" maxLength:"500"`
		AWSSessionToken    string `json:"aws_session_token,omitempty" maxLength:"2000"`
		GCPServiceAccount  string `json:"gcp_service_account,omitempty" maxLength:"20000"`
	}
}

type connectionPath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

type updateConnectionInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		Name               string `json:"name,omitempty" maxLength:"100"`
		BaseURL            string `json:"base_url,omitempty" maxLength:"500"`
		Token              string `json:"token,omitempty" maxLength:"1000"`
		GitHubAppID        string `json:"github_app_id,omitempty" maxLength:"100"`
		GitHubInstallation string `json:"github_installation_id,omitempty" maxLength:"100"`
		GitHubPrivateKey   string `json:"github_private_key,omitempty" maxLength:"20000"`
		AWSAccessKey       string `json:"aws_access_key,omitempty" maxLength:"200"`
		AWSSecretKey       string `json:"aws_secret_key,omitempty" maxLength:"500"`
		AWSSessionToken    string `json:"aws_session_token,omitempty" maxLength:"2000"`
		GCPServiceAccount  string `json:"gcp_service_account,omitempty" maxLength:"20000"`
	}
}

type testConnectionInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body *struct {
		ForgejoScope string `json:"forgejo_scope,omitempty" enum:"repo,org" doc:"Scope of the extra destination to check"`
		ForgejoRepo  string `json:"forgejo_repo,omitempty" maxLength:"300" doc:"owner/name, or an organization name, to check besides the syncs that use the connection"`
	}
}

type connectionCheckBody struct {
	Target string `json:"target"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

type testConnectionOutput struct {
	Body struct {
		OK       bool                  `json:"ok"`
		Detail   string                `json:"detail"`
		Identity string                `json:"identity,omitempty"`
		Checks   []connectionCheckBody `json:"checks"`
	}
}

type useSyncConnectionInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
	Body    struct {
		ConnectionID string `json:"connection_id,omitempty" doc:"Existing connection to use"`
		Name         string `json:"name,omitempty" maxLength:"100" doc:"Create a connection with this name from the sync's own credential"`
	}
}

type useSyncConnectionOutput struct {
	Body struct {
		Sync       syncBody       `json:"sync"`
		Connection connectionBody `json:"connection"`
	}
}

func connectionFromView(view service.ConnectionView) connectionBody {
	conn := view.Connection
	body := connectionBody{
		ID: conn.ID, Provider: conn.Provider, Name: conn.Name, BaseURL: conn.BaseURL, Auth: view.Auth,
		CreatedBy: conn.CreatedBy, CreatedAt: conn.CreatedAt, UpdatedAt: conn.UpdatedAt,
		LastUsedAt: conn.LastUsedAt, SyncCount: view.UsageCount,
	}
	if view.Usage != nil {
		body.Syncs = []connectionUsageBody{}
		for _, usage := range view.Usage {
			body.Syncs = append(body.Syncs, connectionUsageBody{
				SyncID: usage.SyncID, Project: usage.ProjectSlug, Environment: usage.Environment,
			})
		}
	}
	return body
}

func (s *Server) registerConnectionRoutes() error {
	base := "/api/v1/orgs/{org}/connections"
	if err := s.registry.Declare(http.MethodGet, base, authz.OrgRead, "list sync connections (no credentials)"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-connections", Method: http.MethodGet, Path: base,
		Summary: "List sync connections", Tags: []string{"connections"},
		Description: "Org-level provider credentials that syncs can share. Credentials are never returned.",
	}, func(ctx context.Context, in *listConnectionsInput) (*connectionsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListSyncConnections(ctx, actor, in.Org, in.Provider)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &connectionsOutput{}
		out.Body.Connections = []connectionBody{}
		for _, view := range views {
			out.Body.Connections = append(out.Body.Connections, connectionFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.SyncsManage, "create sync connections"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-connection", Method: http.MethodPost, Path: base,
		Summary: "Create a sync connection", Tags: []string{"connections"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createConnectionInput) (*connectionOutput, error) {
		actor, _ := actorFrom(ctx)
		b := in.Body
		view, err := s.svc.CreateSyncConnection(ctx, actor, in.Org, service.ConnectionInput{
			Provider: b.Provider, Name: b.Name, BaseURL: b.BaseURL, Token: b.Token,
			GitHubAppID: b.GitHubAppID, GitHubInstallation: b.GitHubInstallation, GitHubPrivateKey: b.GitHubPrivateKey,
			AWSAccessKey: b.AWSAccessKey, AWSSecretKey: b.AWSSecretKey, AWSSessionToken: b.AWSSessionToken,
			GCPServiceAccount: b.GCPServiceAccount,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &connectionOutput{Body: connectionFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, base+"/{id}", authz.SyncsManage, "rename or rotate sync connections"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-connection", Method: http.MethodPatch, Path: base + "/{id}",
		Summary: "Rename, move, or rotate a sync connection", Tags: []string{"connections"},
		Description: "Empty fields are unchanged. Any credential field replaces the whole credential (rotation); every sync using the connection picks it up on its next run.",
	}, func(ctx context.Context, in *updateConnectionInput) (*connectionOutput, error) {
		actor, _ := actorFrom(ctx)
		b := in.Body
		view, err := s.svc.UpdateSyncConnection(ctx, actor, in.Org, in.ID, service.ConnectionInput{
			Name: b.Name, BaseURL: b.BaseURL, Token: b.Token,
			GitHubAppID: b.GitHubAppID, GitHubInstallation: b.GitHubInstallation, GitHubPrivateKey: b.GitHubPrivateKey,
			AWSAccessKey: b.AWSAccessKey, AWSSecretKey: b.AWSSecretKey, AWSSessionToken: b.AWSSessionToken,
			GCPServiceAccount: b.GCPServiceAccount,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &connectionOutput{Body: connectionFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.SyncsManage, "delete sync connections"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-connection", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a sync connection", Tags: []string{"connections"}, DefaultStatus: http.StatusNoContent,
		Description: "Refused with 409 connection_in_use while any sync uses the connection.",
	}, func(ctx context.Context, in *connectionPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSyncConnection(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPost, base+"/{id}/test", authz.SyncsManage, "test sync connections"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "test-connection", Method: http.MethodPost, Path: base + "/{id}/test",
		Summary: "Test a sync connection", Tags: []string{"connections"},
		Description: "Makes cheap authenticated calls to the provider. A rejected credential returns ok=false with the reason. For Forgejo it also checks Actions access in every destination its syncs use, plus an optional extra one.",
	}, func(ctx context.Context, in *testConnectionInput) (*testConnectionOutput, error) {
		actor, _ := actorFrom(ctx)
		var target *service.ConnectionTestTarget
		if in.Body != nil && in.Body.ForgejoRepo != "" {
			target = &service.ConnectionTestTarget{ForgejoScope: in.Body.ForgejoScope, ForgejoRepo: in.Body.ForgejoRepo}
		}
		result, err := s.svc.TestSyncConnection(ctx, actor, in.Org, in.ID, target)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &testConnectionOutput{}
		out.Body.OK = result.OK
		out.Body.Detail = result.Detail
		out.Body.Identity = result.Identity
		out.Body.Checks = []connectionCheckBody{}
		for _, check := range result.Checks {
			out.Body.Checks = append(out.Body.Checks, connectionCheckBody{Target: check.Target, OK: check.OK, Detail: check.Detail})
		}
		return out, nil
	})

	syncConnection := "/api/v1/orgs/{org}/projects/{project}/syncs/{id}/connection"
	if err := s.registry.Declare(http.MethodPut, syncConnection, authz.SyncsManage, "move a sync onto a connection"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "use-sync-connection", Method: http.MethodPut, Path: syncConnection,
		Summary: "Move a sync onto a connection", Tags: []string{"syncs", "connections"},
		Description: "With connection_id, the sync uses that connection and its own credential is dropped. With name, a new connection is created from the sync's credential first (needs org-wide syncs:manage).",
	}, func(ctx context.Context, in *useSyncConnectionInput) (*useSyncConnectionOutput, error) {
		actor, _ := actorFrom(ctx)
		view, conn, err := s.svc.UseSyncConnection(ctx, actor, in.Org, in.Project, in.ID,
			in.Body.ConnectionID, in.Body.Name, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &useSyncConnectionOutput{}
		out.Body.Sync = syncFromView(*view)
		out.Body.Connection = connectionFromView(*conn)
		return out, nil
	})
	return nil
}
