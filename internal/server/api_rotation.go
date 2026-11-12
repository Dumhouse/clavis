package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type rotationPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	ID      string `path:"id"`
}

type createRotationInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Key      string         `json:"key" minLength:"1" maxLength:"256"`
		Provider string         `json:"provider" enum:"postgres,aws,webhook"`
		Config   map[string]any `json:"config"`
		Interval int            `json:"interval_seconds" minimum:"60" maximum:"7776000"`
		Enabled  bool           `json:"enabled,omitempty" default:"true"`
	}
}

type rotationPolicyBody struct {
	ID          string     `json:"id"`
	Environment string     `json:"environment"`
	Key         string     `json:"key"`
	Provider    string     `json:"provider"`
	Interval    int        `json:"interval_seconds"`
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	NextRunAt   time.Time  `json:"next_run_at"`
	LastStatus  string     `json:"last_status,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type rotationPoliciesOutput struct {
	Body struct {
		Policies []rotationPolicyBody `json:"policies"`
	}
}

type rotationPolicyOutput struct {
	Body rotationPolicyBody
}

type rotationRunBody struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Message    string     `json:"message,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type rotationRunsOutput struct {
	Body struct {
		Runs []rotationRunBody `json:"runs"`
	}
}

type rotationStatusInput struct {
	Org       string `path:"org"`
	Project   string `path:"project"`
	Env       string `path:"env"`
	StaleDays int    `query:"stale_days" minimum:"1" maximum:"3650" default:"90"`
}

type rotationStatusSecretBody struct {
	Key            string    `json:"key"`
	OwnerGroupID   string    `json:"owner_group_id,omitempty"`
	OwnerGroupName string    `json:"owner_group_name,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	AgeDays        int       `json:"age_days"`
}

type rotationStatusOutput struct {
	Body struct {
		Environment string                     `json:"environment"`
		StaleDays   int                        `json:"stale_days"`
		Managed     int                        `json:"managed"`
		Unmanaged   int                        `json:"unmanaged"`
		Stale       []rotationStatusSecretBody `json:"stale"`
	}
}

type rotationEnabledInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	ID      string `path:"id"`
	Body    struct {
		Enabled bool `json:"enabled"`
	}
}

func (s *Server) registerRotationRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/rotations"
	if err := s.registry.Declare(http.MethodGet, base, authz.SecretsReadNames, "list rotation policies"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-rotations", Method: http.MethodGet, Path: base,
		Summary: "List rotation policies", Tags: []string{"rotation"},
	}, func(ctx context.Context, in *overlayPath) (*rotationPoliciesOutput, error) {
		actor, _ := actorFrom(ctx)
		policies, err := s.svc.ListRotationPolicies(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &rotationPoliciesOutput{}
		for _, policy := range policies {
			out.Body.Policies = append(out.Body.Policies, rotationFromService(policy))
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.SecretsWrite, "create rotation policies"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-rotation", Method: http.MethodPost, Path: base,
		Summary: "Create a rotation policy", Tags: []string{"rotation"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createRotationInput) (*rotationPolicyOutput, error) {
		actor, _ := actorFrom(ctx)
		policy, err := s.svc.CreateRotationPolicy(ctx, actor, in.Org, in.Project, in.Env, in.Body.Key,
			in.Body.Provider, in.Body.Config, in.Body.Interval, in.Body.Enabled, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &rotationPolicyOutput{Body: rotationFromService(*policy)}, nil
	})
	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.SecretsWrite, "delete rotation policies"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-rotation", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a rotation policy", Tags: []string{"rotation"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *rotationPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeRotationPolicy(ctx, actor, in.Org, in.Project, in.Env, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	statusPath := base + "/status"
	if err := s.registry.Declare(http.MethodGet, statusPath, authz.SecretsReadNames, "read rotation status"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "rotation-status", Method: http.MethodGet, Path: statusPath,
		Summary: "Read rotation status for manual secrets", Tags: []string{"rotation"},
	}, func(ctx context.Context, in *rotationStatusInput) (*rotationStatusOutput, error) {
		actor, _ := actorFrom(ctx)
		status, err := s.svc.RotationStatusForEnv(ctx, actor, in.Org, in.Project, in.Env, in.StaleDays)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &rotationStatusOutput{}
		out.Body.Environment = status.Environment
		out.Body.StaleDays = status.StaleDays
		out.Body.Managed = status.Managed
		out.Body.Unmanaged = status.Unmanaged
		for _, secret := range status.Stale {
			out.Body.Stale = append(out.Body.Stale, rotationStatusSecretBody{
				Key: secret.Key, OwnerGroupID: secret.OwnerGroupID,
				OwnerGroupName: secret.OwnerGroupName, UpdatedAt: secret.UpdatedAt,
				AgeDays: secret.AgeDays,
			})
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/{id}/run", authz.SecretsWrite, "run rotation now"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "run-rotation", Method: http.MethodPost, Path: base + "/{id}/run",
		Summary: "Run a rotation now", Tags: []string{"rotation"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *rotationPath) (*rotationRunsOutput, error) {
		actor, _ := actorFrom(ctx)
		run, err := s.svc.RunRotation(ctx, actor, in.Org, in.Project, in.Env, in.ID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &rotationRunsOutput{}
		out.Body.Runs = []rotationRunBody{rotationRunFromService(*run)}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/{id}/enabled", authz.SecretsWrite, "toggle rotation policies"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "toggle-rotation", Method: http.MethodPost, Path: base + "/{id}/enabled",
		Summary: "Enable or disable a rotation policy", Tags: []string{"rotation"},
	}, func(ctx context.Context, in *rotationEnabledInput) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.SetRotationEnabled(ctx, actor, in.Org, in.Project, in.Env, in.ID, in.Body.Enabled, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	if err := s.registry.Declare(http.MethodGet, base+"/{id}/runs", authz.SecretsReadNames, "list rotation runs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-rotation-runs", Method: http.MethodGet, Path: base + "/{id}/runs",
		Summary: "List rotation runs", Tags: []string{"rotation"},
	}, func(ctx context.Context, in *rotationPath) (*rotationRunsOutput, error) {
		actor, _ := actorFrom(ctx)
		runs, err := s.svc.ListRotationRuns(ctx, actor, in.Org, in.Project, in.Env, in.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &rotationRunsOutput{}
		for _, run := range runs {
			out.Body.Runs = append(out.Body.Runs, rotationRunFromService(run))
		}
		return out, nil
	})
	return nil
}

func rotationFromService(policy service.RotationPolicyView) rotationPolicyBody {
	return rotationPolicyBody{
		ID: policy.ID, Environment: policy.Environment, Key: policy.SecretKey,
		Provider: policy.Provider, Interval: policy.Interval, Enabled: policy.Enabled,
		LastRunAt: policy.LastRunAt, NextRunAt: policy.NextRunAt,
		LastStatus: policy.LastStatus, LastError: policy.LastError,
	}
}

func rotationRunFromService(run service.RotationRunView) rotationRunBody {
	return rotationRunBody{
		ID: run.ID, Status: run.Status, Phase: run.Phase, Message: run.Message,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}
