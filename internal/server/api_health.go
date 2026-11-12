package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type healthInput struct {
	Org     string `path:"org"`
	Project string `query:"project" maxLength:"60"`
}

type healthFindingBody struct {
	Category string `json:"category"`
	Level    string `json:"level"`
	Message  string `json:"message"`
	Project  string `json:"project,omitempty"`
	Env      string `json:"env,omitempty"`
	Key      string `json:"key,omitempty"`
	Subject  string `json:"subject,omitempty"`
}

type healthOutput struct {
	Body struct {
		Score    int                 `json:"score"`
		Checked  int                 `json:"checked"`
		Findings []healthFindingBody `json:"findings"`
	}
}

func (s *Server) registerHealthRoutes() error {
	path := "/api/v1/orgs/{org}/health"
	if err := s.registry.Declare(http.MethodGet, path, authz.RolesManage, "organization health report"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "health-report", Method: http.MethodGet, Path: path,
		Summary: "Organization health report", Tags: []string{"health"},
	}, func(ctx context.Context, in *healthInput) (*healthOutput, error) {
		actor, _ := actorFrom(ctx)
		report, err := s.svc.Health(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &healthOutput{}
		out.Body.Score = report.Score
		out.Body.Checked = report.Checked
		for _, finding := range report.Findings {
			out.Body.Findings = append(out.Body.Findings, healthFindingBody{
				Category: finding.Category, Level: finding.Level, Message: finding.Message,
				Project: finding.Project, Env: finding.Env, Key: finding.Key, Subject: finding.Subject,
			})
		}
		return out, nil
	})
	return nil
}
