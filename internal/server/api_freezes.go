package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type freezePath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	ID      string `path:"id"`
}

type createFreezeInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		StartsAt time.Time `json:"starts_at,omitempty"`
		EndsAt   time.Time `json:"ends_at"`
		Reason   string    `json:"reason" minLength:"1" maxLength:"512"`
	}
}

type freezeBody struct {
	ID        string    `json:"id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"created_by"`
	Active    bool      `json:"active"`
}

type freezesOutput struct {
	Body struct {
		Freezes []freezeBody `json:"freezes"`
	}
}

type freezeOutput struct {
	Body freezeBody
}

func (s *Server) registerFreezeRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/freezes"
	if err := s.registry.Declare(http.MethodGet, base, authz.EnvironmentsRead, "list environment freezes"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-freezes", Method: http.MethodGet, Path: base,
		Summary: "List environment freezes", Tags: []string{"environments"},
	}, func(ctx context.Context, in *environmentPath) (*freezesOutput, error) {
		actor, _ := actorFrom(ctx)
		freezes, err := s.svc.ListFreezes(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &freezesOutput{}
		for _, freeze := range freezes {
			out.Body.Freezes = append(out.Body.Freezes, freezeFromService(freeze))
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.EnvironmentsManage, "schedule environment freezes"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-freeze", Method: http.MethodPost, Path: base,
		Summary: "Schedule an environment freeze", Tags: []string{"environments"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createFreezeInput) (*freezeOutput, error) {
		actor, _ := actorFrom(ctx)
		freeze, err := s.svc.CreateFreeze(ctx, actor, in.Org, in.Project, in.Env,
			in.Body.StartsAt, in.Body.EndsAt, in.Body.Reason, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &freezeOutput{Body: freezeFromService(*freeze)}, nil
	})
	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.EnvironmentsManage, "delete environment freezes"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-freeze", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete an environment freeze", Tags: []string{"environments"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *freezePath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteFreeze(ctx, actor, in.Org, in.Project, in.Env, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func freezeFromService(freeze service.FreezeView) freezeBody {
	return freezeBody{
		ID: freeze.ID, StartsAt: freeze.StartsAt, EndsAt: freeze.EndsAt,
		Reason: freeze.Reason, CreatedBy: freeze.CreatedBy, Active: freeze.Active,
	}
}
