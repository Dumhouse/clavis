package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type promotionPlanInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Source string `json:"source" minLength:"1" maxLength:"60"`
		Target string `json:"target" minLength:"1" maxLength:"60"`
	}
}

type promotionEntryBody struct {
	Key             string `json:"key"`
	Status          string `json:"status"`
	Kind            string `json:"kind,omitempty"`
	SourceKind      string `json:"source_kind,omitempty"`
	TargetKind      string `json:"target_kind,omitempty"`
	SchemaError     string `json:"schema_error,omitempty"`
	SourceInherited bool   `json:"source_inherited,omitempty"`
	TargetInherited bool   `json:"target_inherited,omitempty"`
}

type promotionPlanOutput struct {
	Body struct {
		Source           string               `json:"source"`
		Target           string               `json:"target"`
		SourceType       string               `json:"source_type"`
		TargetType       string               `json:"target_type"`
		TargetProtected  bool                 `json:"target_protected"`
		ApprovalRequired bool                 `json:"approval_required"`
		Entries          []promotionEntryBody `json:"entries"`
	}
}

type promotionApplyInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Source   string   `json:"source" minLength:"1" maxLength:"60"`
		Target   string   `json:"target" minLength:"1" maxLength:"60"`
		Keys     []string `json:"keys" maxItems:"1000"`
		Snapshot bool     `json:"snapshot,omitempty" default:"true"`
		Message  string   `json:"message,omitempty" maxLength:"512"`
		Request  bool     `json:"request,omitempty" doc:"Create an approval request instead of writing"`
	}
}

type promotionRequestBody struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Target      string   `json:"target"`
	Keys        []string `json:"keys"`
	Status      string   `json:"status"`
	RequestedBy string   `json:"requested_by"`
	RequestedAt string   `json:"requested_at"`
	DecidedBy   string   `json:"decided_by,omitempty"`
	DecidedAt   string   `json:"decided_at,omitempty"`
}

type promotionResultOutput struct {
	Body struct {
		Applied    int                   `json:"applied"`
		SnapshotID string                `json:"snapshot_id,omitempty"`
		Request    *promotionRequestBody `json:"request,omitempty"`
	}
}

type promotionRequestsOutput struct {
	Body struct {
		Requests []promotionRequestBody `json:"requests"`
	}
}

type promotionRequestPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

type promotionRequestsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Status  string `query:"status" enum:"pending,approved,rejected" default:"pending"`
}

func (s *Server) registerPromotionRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}"
	planPath := base + "/promotions:plan"
	if err := s.registry.Declare(http.MethodPost, planPath, authz.EnvironmentsRead, "plan an environment promotion"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "plan-promotion", Method: http.MethodPost, Path: planPath,
		Summary: "Plan an environment promotion", Tags: []string{"promotions"},
	}, func(ctx context.Context, in *promotionPlanInput) (*promotionPlanOutput, error) {
		actor, _ := actorFrom(ctx)
		plan, err := s.svc.PlanPromotion(ctx, actor, in.Org, in.Project, in.Body.Source, in.Body.Target, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &promotionPlanOutput{}
		out.Body.Source = plan.Source
		out.Body.Target = plan.Target
		out.Body.SourceType = plan.SourceType
		out.Body.TargetType = plan.TargetType
		out.Body.TargetProtected = plan.TargetProtected
		out.Body.ApprovalRequired = plan.ApprovalRequired
		for _, entry := range plan.Entries {
			out.Body.Entries = append(out.Body.Entries, promotionEntryBody{
				Key: entry.Key, Status: entry.Status, Kind: entry.Kind,
				SourceKind: entry.SourceKind, TargetKind: entry.TargetKind,
				SchemaError: entry.SchemaError, SourceInherited: entry.SourceInherited,
				TargetInherited: entry.TargetInherited,
			})
		}
		return out, nil
	})

	applyPath := base + "/promotions"
	if err := s.registry.Declare(http.MethodPost, applyPath, authz.SecretsWrite, "apply or request an environment promotion"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "apply-promotion", Method: http.MethodPost, Path: applyPath,
		Summary: "Apply or request an environment promotion", Tags: []string{"promotions"},
	}, func(ctx context.Context, in *promotionApplyInput) (*promotionResultOutput, error) {
		actor, _ := actorFrom(ctx)
		out := &promotionResultOutput{}
		if in.Body.Request {
			request, err := s.svc.CreatePromotionRequest(ctx, actor, in.Org, in.Project, in.Body.Source, in.Body.Target, in.Body.Keys, metaFrom(ctx))
			if err != nil {
				return nil, s.apiError(ctx, err)
			}
			body := promotionRequestFromService(request)
			out.Body.Request = &body
			return out, nil
		}
		result, err := s.svc.Promote(ctx, actor, in.Org, in.Project, in.Body.Source, in.Body.Target, in.Body.Keys, in.Body.Snapshot, in.Body.Message, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out.Body.Applied = result.Applied
		out.Body.SnapshotID = result.SnapshotID
		return out, nil
	})

	requestsPath := base + "/promotion-requests"
	if err := s.registry.Declare(http.MethodGet, requestsPath, authz.EnvironmentsRead, "list promotion requests"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-promotion-requests", Method: http.MethodGet, Path: requestsPath,
		Summary: "List promotion requests", Tags: []string{"promotions"},
	}, func(ctx context.Context, in *promotionRequestsInput) (*promotionRequestsOutput, error) {
		actor, _ := actorFrom(ctx)
		requests, err := s.svc.ListPromotionRequests(ctx, actor, in.Org, in.Project, in.Status)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &promotionRequestsOutput{}
		for _, request := range requests {
			out.Body.Requests = append(out.Body.Requests, promotionRequestFromService(&request))
		}
		return out, nil
	})

	approvePath := requestsPath + "/{id}/approve"
	if err := s.registry.Declare(http.MethodPost, approvePath, authz.SettingsManage, "approve a promotion request"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "approve-promotion-request", Method: http.MethodPost, Path: approvePath,
		Summary: "Approve a promotion request", Tags: []string{"promotions"},
	}, func(ctx context.Context, in *promotionRequestPath) (*promotionResultOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.ApprovePromotionRequest(ctx, actor, in.Org, in.Project, in.ID, true, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &promotionResultOutput{}
		out.Body.Applied = result.Applied
		out.Body.SnapshotID = result.SnapshotID
		if result.Request != nil {
			body := promotionRequestFromService(result.Request)
			out.Body.Request = &body
		}
		return out, nil
	})

	rejectPath := requestsPath + "/{id}/reject"
	if err := s.registry.Declare(http.MethodPost, rejectPath, authz.SettingsManage, "reject a promotion request"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "reject-promotion-request", Method: http.MethodPost, Path: rejectPath,
		Summary: "Reject a promotion request", Tags: []string{"promotions"},
	}, func(ctx context.Context, in *promotionRequestPath) (*promotionRequestsOutput, error) {
		actor, _ := actorFrom(ctx)
		request, err := s.svc.RejectPromotionRequest(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &promotionRequestsOutput{}
		out.Body.Requests = []promotionRequestBody{promotionRequestFromService(request)}
		return out, nil
	})
	return nil
}

func promotionRequestFromService(view *service.PromotionRequestView) promotionRequestBody {
	return promotionRequestBody{
		ID: view.ID, Source: view.Source, Target: view.Target, Keys: view.Keys,
		Status: view.Status, RequestedBy: view.RequestedBy, RequestedAt: view.RequestedAt,
		DecidedBy: view.DecidedBy, DecidedAt: view.DecidedAt,
	}
}
