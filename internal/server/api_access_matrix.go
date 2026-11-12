package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type matrixRowBody struct {
	SubjectKind string   `json:"subject_kind"`
	SubjectID   string   `json:"subject_id"`
	Name        string   `json:"name"`
	Email       string   `json:"email,omitempty"`
	Role        string   `json:"role,omitempty"`
	MemberCount int      `json:"member_count,omitempty"`
	Permissions []string `json:"permissions"`
}

type matrixOutput struct {
	Body struct {
		Permissions []string        `json:"permissions"`
		ProjectID   string          `json:"project_id,omitempty"`
		Rows        []matrixRowBody `json:"rows"`
	}
}

type matrixInput struct {
	Org         string `path:"org"`
	Project     string `query:"project,omitempty" maxLength:"60"`
	Environment string `query:"environment,omitempty" maxLength:"60"`
}

type bindingViewBody struct {
	Role        string     `json:"role"`
	Source      string     `json:"source"`
	SubjectKind string     `json:"subject_kind"`
	SubjectID   string     `json:"subject_id"`
	ProjectID   string     `json:"project_id,omitempty"`
	Environment string     `json:"environment,omitempty"`
	Path        string     `json:"path,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Grants      bool       `json:"grants"`
}

type explainOutput struct {
	Body struct {
		Allowed   bool              `json:"allowed"`
		Reason    string            `json:"reason"`
		Subject   string            `json:"subject"`
		ProjectID string            `json:"project_id,omitempty"`
		Matched   *bindingViewBody  `json:"matched,omitempty"`
		Bindings  []bindingViewBody `json:"bindings"`
	}
}

type explainInput struct {
	Org         string `path:"org"`
	SubjectKind string `query:"subject_kind,omitempty" enum:"user,group" default:"user"`
	SubjectID   string `query:"subject_id" minLength:"1"`
	Permission  string `query:"permission" minLength:"1"`
	Project     string `query:"project,omitempty" maxLength:"60"`
	Environment string `query:"environment,omitempty" maxLength:"60"`
	Path        string `query:"path,omitempty" maxLength:"512"`
}

func (s *Server) registerAccessMatrixRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/access/matrix", authz.RolesManage, "view the effective access matrix"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "access-matrix", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/access/matrix",
		Summary: "Effective access matrix", Tags: []string{"access"},
	}, func(ctx context.Context, in *matrixInput) (*matrixOutput, error) {
		actor, _ := actorFrom(ctx)
		matrix, err := s.svc.AccessMatrix(ctx, actor, in.Org, service.MatrixInput{
			ProjectSlug: in.Project, Environment: in.Environment,
		})
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &matrixOutput{}
		out.Body.Permissions = matrix.Permissions
		out.Body.ProjectID = matrix.ProjectID
		for _, row := range matrix.Rows {
			out.Body.Rows = append(out.Body.Rows, matrixRowBody{
				SubjectKind: row.SubjectKind, SubjectID: row.SubjectID, Name: row.Name,
				Email: row.Email, Role: row.Role, MemberCount: row.MemberCount, Permissions: row.Permissions,
			})
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/access/explain", authz.RolesManage, "explain why a subject has access"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "access-explain", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/access/explain",
		Summary: "Explain access", Tags: []string{"access"},
	}, func(ctx context.Context, in *explainInput) (*explainOutput, error) {
		actor, _ := actorFrom(ctx)
		explanation, err := s.svc.ExplainAccess(ctx, actor, in.Org, service.ExplainInput{
			SubjectKind: in.SubjectKind, SubjectID: in.SubjectID, Permission: in.Permission,
			ProjectSlug: in.Project, Environment: in.Environment, Path: in.Path,
		})
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &explainOutput{}
		out.Body.Allowed = explanation.Allowed
		out.Body.Reason = explanation.Reason
		out.Body.Subject = explanation.Subject
		out.Body.ProjectID = explanation.ProjectID
		if explanation.Matched != nil {
			body := bindingViewFromService(*explanation.Matched)
			out.Body.Matched = &body
		}
		for _, binding := range explanation.Bindings {
			out.Body.Bindings = append(out.Body.Bindings, bindingViewFromService(binding))
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/access-review", authz.RolesManage, "who can read values in an environment"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "access-review", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/access-review",
		Summary: "Access review report", Tags: []string{"access"},
	}, func(ctx context.Context, in *accessReviewInput) (*accessReviewOutput, error) {
		actor, _ := actorFrom(ctx)
		rows, err := s.svc.AccessReview(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &accessReviewOutput{}
		for _, row := range rows {
			out.Body = append(out.Body, accessReviewRowBody{
				SubjectKind: row.SubjectKind, SubjectID: row.SubjectID, Name: row.Name,
				Email: row.Email, Role: row.Role,
			})
		}
		return out, nil
	})
	return nil
}

type accessReviewInput struct {
	Org     string `path:"org"`
	Project string `query:"project" maxLength:"60"`
	Env     string `query:"env" maxLength:"60" default:"prod"`
}

type accessReviewRowBody struct {
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`
}

type accessReviewOutput struct {
	Body []accessReviewRowBody
}

func bindingViewFromService(view service.BindingView) bindingViewBody {
	return bindingViewBody{
		Role: view.Role, Source: view.Source, SubjectKind: view.SubjectKind, SubjectID: view.SubjectID,
		ProjectID: view.ProjectID, Environment: view.Environment, Path: view.Path,
		ExpiresAt: view.ExpiresAt, Grants: view.Grants,
	}
}
