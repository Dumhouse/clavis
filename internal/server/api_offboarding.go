package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type memberOffboardingPath struct {
	Org  string `path:"org"`
	User string `path:"user"`
}

type offboardingTaskBody struct {
	ID          string    `json:"id"`
	ProjectSlug string    `json:"project"`
	EnvSlug     string    `json:"environment"`
	Key         string    `json:"key"`
	Status      string    `json:"status"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type offboardingBody struct {
	UserID   string                `json:"user_id"`
	Email    string                `json:"email,omitempty"`
	Name     string                `json:"name,omitempty"`
	Done     int                   `json:"done"`
	Total    int                   `json:"total"`
	Exported int                   `json:"exported"`
	Tasks    []offboardingTaskBody `json:"tasks"`
}

type offboardingOutput struct {
	Body offboardingBody
}

type markOffboardingInput struct {
	Org  string `path:"org"`
	User string `path:"user"`
	ID   string `path:"id"`
	Body struct {
		Status string `json:"status" enum:"rotated,safe"`
	}
}

func (s *Server) registerOffboardingRoutes() error {
	base := "/api/v1/orgs/{org}/members/{user}/offboarding"
	if err := s.registry.Declare(http.MethodGet, base, authz.MembersManage, "read offboarding progress"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "offboarding-report", Method: http.MethodGet, Path: base,
		Summary: "Read offboarding progress", Tags: []string{"members"},
	}, func(ctx context.Context, in *memberOffboardingPath) (*offboardingOutput, error) {
		actor, _ := actorFrom(ctx)
		report, err := s.svc.OffboardingReport(ctx, actor, in.Org, in.User)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &offboardingOutput{Body: offboardingFromService(*report)}, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.MembersManage, "start offboarding review"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "start-offboarding", Method: http.MethodPost, Path: base,
		Summary: "Start an offboarding review", Tags: []string{"members"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *memberOffboardingPath) (*offboardingOutput, error) {
		actor, _ := actorFrom(ctx)
		report, err := s.svc.StartOffboarding(ctx, actor, in.Org, in.User, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &offboardingOutput{Body: offboardingFromService(*report)}, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/{id}", authz.MembersManage, "mark offboarding tasks"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "mark-offboarding-task", Method: http.MethodPost, Path: base + "/{id}",
		Summary: "Mark an offboarding task rotated or safe", Tags: []string{"members"},
	}, func(ctx context.Context, in *markOffboardingInput) (*offboardingOutput, error) {
		actor, _ := actorFrom(ctx)
		report, err := s.svc.MarkOffboardingTask(ctx, actor, in.Org, in.User, in.ID, in.Body.Status, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &offboardingOutput{Body: offboardingFromService(*report)}, nil
	})
	return nil
}

func offboardingFromService(report service.OffboardingReport) offboardingBody {
	out := offboardingBody{UserID: report.UserID, Email: report.Email, Name: report.Name,
		Done: report.Done, Total: report.Total, Exported: report.Exported}
	for _, task := range report.Tasks {
		out.Tasks = append(out.Tasks, offboardingTaskBody{
			ID: task.ID, ProjectSlug: task.ProjectSlug, EnvSlug: task.EnvSlug,
			Key: task.Key, Status: task.Status, UpdatedAt: task.UpdatedAt,
		})
	}
	return out
}
