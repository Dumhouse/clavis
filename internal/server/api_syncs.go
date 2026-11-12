package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/internal/store"
)

type syncBody struct {
	ID           string     `json:"id"`
	Environment  string     `json:"environment"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status"`
	Confirmed    bool       `json:"confirmed"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

type syncsOutput struct {
	Body struct {
		Syncs []syncBody `json:"syncs"`
	}
}

type syncOutput struct {
	Body syncBody
}

type createSyncInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Environment       string `json:"environment" minLength:"1" maxLength:"60"`
		Provider          string `json:"provider,omitempty"`
		Endpoint          string `json:"endpoint,omitempty" maxLength:"2048"`
		Secret            string `json:"secret,omitempty" maxLength:"500"`
		BaseURL           string `json:"base_url,omitempty" maxLength:"500"`
		GitLabProject     string `json:"gitlab_project,omitempty" maxLength:"500"`
		Token             string `json:"token,omitempty" maxLength:"1000"`
		Protected         bool   `json:"protected,omitempty"`
		Masked            bool   `json:"masked,omitempty"`
		GitHubRepo        string `json:"github_repo,omitempty" maxLength:"500"`
		GitHubEnvironment string `json:"github_environment,omitempty" maxLength:"200"`
		GitHubScope       string `json:"github_scope,omitempty" enum:"repo,environment,org"`
	}
}

type syncPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

type runSyncInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
	Body    struct {
		DryRun  bool `json:"dry_run,omitempty"`
		Confirm bool `json:"confirm,omitempty"`
	}
}

type syncRunBody struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	DryRun      bool       `json:"dry_run"`
	SecretCount int        `json:"secret_count"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type syncPreviewBody struct {
	SyncID      string   `json:"sync_id"`
	Destination string   `json:"destination"`
	Environment string   `json:"environment"`
	Keys        []string `json:"keys"`
	Count       int      `json:"count"`
	Confirmed   bool     `json:"confirmed"`
}

type runSyncOutput struct {
	Body struct {
		Run     syncRunBody      `json:"run"`
		Preview *syncPreviewBody `json:"preview,omitempty"`
	}
}

type syncRunsOutput struct {
	Body struct {
		Runs []syncRunBody `json:"runs"`
	}
}

func (s *Server) registerSyncRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/syncs"
	if err := s.registry.Declare(http.MethodGet, base, authz.SyncsManage, "list project syncs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-syncs", Method: http.MethodGet, Path: base,
		Summary: "List syncs", Tags: []string{"syncs"},
	}, func(ctx context.Context, in *projectPath) (*syncsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListSyncs(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &syncsOutput{}
		out.Body.Syncs = []syncBody{}
		for _, view := range views {
			out.Body.Syncs = append(out.Body.Syncs, syncFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.SyncsManage, "create project syncs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-sync", Method: http.MethodPost, Path: base,
		Summary: "Create a sync", Tags: []string{"syncs"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSyncInput) (*syncOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.CreateSync(ctx, actor, in.Org, in.Project, service.SyncInput{
			EnvironmentSlug: in.Body.Environment, Provider: in.Body.Provider,
			Endpoint: in.Body.Endpoint, Secret: in.Body.Secret,
			BaseURL: in.Body.BaseURL, GitLabProject: in.Body.GitLabProject,
			Token: in.Body.Token, Protected: in.Body.Protected, Masked: in.Body.Masked,
			GitHubRepo: in.Body.GitHubRepo, GitHubEnvironment: in.Body.GitHubEnvironment,
			GitHubScope: in.Body.GitHubScope,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &syncOutput{Body: syncFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.SyncsManage, "delete project syncs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-sync", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a sync", Tags: []string{"syncs"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *syncPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSync(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPost, base+"/{id}/run", authz.SyncsManage, "run project syncs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "run-sync", Method: http.MethodPost, Path: base + "/{id}/run",
		Summary: "Preview or run a sync", Tags: []string{"syncs"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *runSyncInput) (*runSyncOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.RunSync(ctx, actor, in.Org, in.Project, in.ID, in.Body.DryRun, in.Body.Confirm, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &runSyncOutput{}
		if result.Run != nil {
			out.Body.Run = syncRunFromStore(result.Run)
		}
		if result.Preview != nil {
			out.Body.Preview = &syncPreviewBody{
				SyncID: result.Preview.SyncID, Destination: result.Preview.Destination,
				Environment: result.Preview.Environment, Keys: result.Preview.Keys,
				Count: result.Preview.Count, Confirmed: result.Preview.Confirmed,
			}
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodGet, base+"/{id}/runs", authz.SyncsManage, "list sync runs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-sync-runs", Method: http.MethodGet, Path: base + "/{id}/runs",
		Summary: "List sync runs", Tags: []string{"syncs"},
	}, func(ctx context.Context, in *syncPath) (*syncRunsOutput, error) {
		actor, _ := actorFrom(ctx)
		runs, err := s.svc.ListSyncRuns(ctx, actor, in.Org, in.Project, in.ID, 50)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &syncRunsOutput{}
		out.Body.Runs = []syncRunBody{}
		for i := range runs {
			out.Body.Runs = append(out.Body.Runs, syncRunFromStore(&runs[i]))
		}
		return out, nil
	})
	return nil
}

func syncFromView(view service.SyncView) syncBody {
	return syncBody{
		ID: view.Sync.ID, Environment: view.Environment, Provider: view.Sync.Provider,
		Status: view.Sync.Status, Confirmed: view.Sync.Confirmed,
		LastSyncedAt: view.Sync.LastSyncedAt, CreatedAt: view.Sync.CreatedAt,
	}
}

func syncRunFromStore(run *store.SyncRun) syncRunBody {
	return syncRunBody{
		ID: run.ID, Status: run.Status, DryRun: run.DryRun, SecretCount: run.SecretCount,
		Error: run.Error, StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}
