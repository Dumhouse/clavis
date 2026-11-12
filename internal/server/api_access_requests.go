package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/email"
	"github.com/dumhouse/clavis/internal/service"
)

type accessRequestPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

type createAccessRequestInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Environment       string `json:"environment" minLength:"1" maxLength:"60"`
		Role              string `json:"role,omitempty" maxLength:"60" default:"Developer"`
		Reason            string `json:"reason" minLength:"1" maxLength:"1024"`
		DurationSeconds   int    `json:"duration_seconds,omitempty" minimum:"300" maximum:"86400" default:"3600"`
		RequiredApprovals int    `json:"required_approvals,omitempty" minimum:"1" maximum:"5" default:"1"`
	}
}

type accessRequestsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Status  string `query:"status" enum:"pending,approved,rejected" default:"pending"`
}

type accessRequestBody struct {
	ID                string     `json:"id"`
	RequesterID       string     `json:"requester_id"`
	Environment       string     `json:"environment"`
	Role              string     `json:"role"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	DurationSeconds   int        `json:"duration_seconds"`
	RequiredApprovals int        `json:"required_approvals"`
	Approvals         []string   `json:"approvals"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	DecidedAt         *time.Time `json:"decided_at,omitempty"`
	AppliedBindingID  string     `json:"applied_binding_id,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type accessRequestsOutput struct {
	Body struct {
		Requests []accessRequestBody `json:"requests"`
	}
}

type accessRequestOutput struct {
	Body accessRequestBody
}

type accessApprovalInput struct {
	Reauth string `header:"X-Clavis-Reauth"`
	Body   struct {
		Token string `json:"token" minLength:"1" maxLength:"4096"`
	}
}

type breakGlassInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Environment     string `json:"environment" minLength:"1" maxLength:"60"`
		Role            string `json:"role,omitempty" maxLength:"60" default:"Admin"`
		Reason          string `json:"reason" minLength:"1" maxLength:"1024"`
		DurationSeconds int    `json:"duration_seconds,omitempty" minimum:"300" maximum:"14400" default:"3600"`
	}
}

type breakGlassOutput struct {
	Body struct {
		ID        string    `json:"id"`
		Role      string    `json:"role"`
		ExpiresAt time.Time `json:"expires_at"`
	}
}

func (s *Server) registerAccessRequestRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/access-requests"
	if err := s.registry.Declare(http.MethodGet, base, authz.EnvironmentsRead, "list access requests"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-access-requests", Method: http.MethodGet, Path: base,
		Summary: "List access requests", Tags: []string{"access"},
	}, func(ctx context.Context, in *accessRequestsInput) (*accessRequestsOutput, error) {
		actor, _ := actorFrom(ctx)
		requests, err := s.svc.ListAccessRequests(ctx, actor, in.Org, in.Project, in.Status)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &accessRequestsOutput{}
		for _, request := range requests {
			out.Body.Requests = append(out.Body.Requests, accessRequestFromService(request))
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.EnvironmentsRead, "request temporary access"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-access-request", Method: http.MethodPost, Path: base,
		Summary: "Request temporary access", Tags: []string{"access"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createAccessRequestInput) (*accessRequestOutput, error) {
		actor, _ := actorFrom(ctx)
		request, err := s.svc.CreateAccessRequest(ctx, actor, in.Org, in.Project, in.Body.Environment,
			in.Body.Role, in.Body.Reason, in.Body.DurationSeconds, in.Body.RequiredApprovals, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		s.notifyAccessRequest(ctx, in.Org, *request)
		return &accessRequestOutput{Body: accessRequestFromService(*request)}, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/break-glass", authz.SettingsManage, "break-glass access"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "break-glass", Method: http.MethodPost, Path: base + "/break-glass",
		Summary: "Grant immediate break-glass access", Tags: []string{"access"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *breakGlassInput) (*breakGlassOutput, error) {
		actor, _ := actorFrom(ctx)
		binding, err := s.svc.BreakGlass(ctx, actor, in.Org, in.Project, in.Body.Environment,
			in.Body.Role, in.Body.Reason, in.Body.DurationSeconds, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &breakGlassOutput{}
		out.Body.ID = binding.ID
		out.Body.Role = binding.RoleName
		if binding.ExpiresAt != nil {
			out.Body.ExpiresAt = *binding.ExpiresAt
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/{id}/approve", authz.RolesManage, "approve access requests"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "approve-access-request", Method: http.MethodPost, Path: base + "/{id}/approve",
		Summary: "Approve an access request", Tags: []string{"access"},
	}, func(ctx context.Context, in *accessRequestPath) (*accessRequestOutput, error) {
		actor, _ := actorFrom(ctx)
		request, err := s.svc.ApproveAccessRequest(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &accessRequestOutput{Body: accessRequestFromService(*request)}, nil
	})
	if err := s.registry.Declare(http.MethodPost, base+"/{id}/reject", authz.RolesManage, "reject access requests"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "reject-access-request", Method: http.MethodPost, Path: base + "/{id}/reject",
		Summary: "Reject an access request", Tags: []string{"access"},
	}, func(ctx context.Context, in *accessRequestPath) (*accessRequestOutput, error) {
		actor, _ := actorFrom(ctx)
		request, err := s.svc.RejectAccessRequest(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &accessRequestOutput{Body: accessRequestFromService(*request)}, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/access-approvals", "signed email/Slack approval links"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "apply-access-approval", Method: http.MethodPost, Path: "/api/v1/access-approvals",
		Summary: "Apply a signed access approval", Tags: []string{"access"},
	}, func(ctx context.Context, in *accessApprovalInput) (*accessRequestOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		request, err := s.svc.ApplyAccessApproval(ctx, actor, in.Body.Token, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &accessRequestOutput{Body: accessRequestFromService(*request)}, nil
	})
	return nil
}

func (s *Server) notifyAccessRequest(ctx context.Context, orgSlug string, request service.AccessRequestView) {
	approveToken, err := s.svc.SignAccessApproval(request.ID, request.ProjectID, "approve", 72*time.Hour)
	if err != nil {
		return
	}
	rejectToken, err := s.svc.SignAccessApproval(request.ID, request.ProjectID, "reject", 72*time.Hour)
	if err != nil {
		return
	}
	base := strings.TrimRight(s.cfg.Server.BaseURL, "/")
	approveLink := base + "/approvals?token=" + approveToken
	rejectLink := base + "/approvals?token=" + rejectToken
	if s.mailer != nil {
		emails, err := s.svc.AccessRequestApproverEmails(ctx, orgSlug)
		if err == nil {
			for _, address := range emails {
				msg := email.Message{
					To: address, Subject: "Clavis access request needs approval",
					Text: fmt.Sprintf("A Clavis access request needs review.\n\nEnvironment: %s\nRole: %s\nReason: %s\nDuration: %d seconds\n\nApprove: %s\nReject: %s\n\nSign in and confirm your password to apply the decision. Links expire in 72 hours.",
						request.Environment, request.Role, request.Reason, request.DurationSeconds, approveLink, rejectLink),
				}
				if err := s.mailer.Send(ctx, msg); err != nil {
					s.logger.Warn("access request email failed", "to", address, "error", err)
				}
			}
		}
	}
	org, err := s.store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		return
	}
	for _, hook := range service.ParseOrgSettings(org.Settings).Webhooks {
		if !hook.Enabled || (hook.Kind != "slack" && hook.Kind != "teams" && hook.Kind != "discord") {
			continue
		}
		if len(hook.Events) > 0 && !webhookEventMatch(hook.Events, "access.request.create") {
			continue
		}
		payload := map[string]any{}
		text := fmt.Sprintf("Clavis access request: %s/%s as %s — %s", request.ProjectSlug, request.Environment, request.Role, request.Reason)
		switch hook.Kind {
		case "slack":
			payload["text"] = text
			payload["blocks"] = []any{
				map[string]any{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": text}},
				map[string]any{"type": "actions", "elements": []any{
					map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Approve"}, "url": approveLink, "style": "primary"},
					map[string]any{"type": "button", "text": map[string]any{"type": "plain_text", "text": "Reject"}, "url": rejectLink, "style": "danger"},
				}},
			}
		case "discord":
			payload["content"] = text + "\nApprove: " + approveLink + "\nReject: " + rejectLink
		case "teams":
			payload["text"] = text + "\nApprove: " + approveLink + "\nReject: " + rejectLink
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, hook.URL, bytes.NewReader(raw))
		if err != nil {
			continue
		}
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest = httpRequest.WithContext(context.Background())
		go func(request *http.Request) {
			client := &http.Client{Timeout: 10 * time.Second}
			response, err := client.Do(request)
			if err == nil {
				_ = response.Body.Close()
			}
		}(httpRequest)
	}
}

func webhookEventMatch(events []string, action string) bool {
	for _, filter := range events {
		if filter == "*" || filter == action || (strings.HasSuffix(filter, "*") && strings.HasPrefix(action, strings.TrimSuffix(filter, "*"))) {
			return true
		}
	}
	return false
}

func accessRequestFromService(request service.AccessRequestView) accessRequestBody {
	return accessRequestBody{
		ID: request.ID, RequesterID: request.RequesterID, Environment: request.Environment,
		Role: request.Role, Reason: request.Reason, Status: request.Status,
		DurationSeconds: request.DurationSeconds, RequiredApprovals: request.RequiredApprovals,
		Approvals: request.Approvals, ExpiresAt: request.ExpiresAt, DecidedAt: request.DecidedAt,
		AppliedBindingID: request.AppliedBindingID, CreatedAt: request.CreatedAt,
	}
}
