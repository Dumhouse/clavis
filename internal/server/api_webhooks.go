package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type webhookBody struct {
	ID            string   `json:"id"`
	URL           string   `json:"url"`
	Description   string   `json:"description,omitempty"`
	Kind          string   `json:"kind"`
	Events        []string `json:"events"`
	ProtectedOnly bool     `json:"protected_only"`
	Enabled       bool     `json:"enabled"`
	SecretSet     bool     `json:"secret_set"`
}

type webhooksOutput struct {
	Body struct {
		Webhooks []webhookBody `json:"webhooks"`
	}
}

type webhookOutput struct {
	Body webhookBody
}

type createWebhookInput struct {
	Org  string `path:"org"`
	Body struct {
		URL           string   `json:"url" minLength:"1" maxLength:"2048"`
		Description   string   `json:"description,omitempty" maxLength:"200"`
		Kind          string   `json:"kind,omitempty" enum:"generic,slack,teams,discord" default:"generic"`
		Events        []string `json:"events,omitempty" maxItems:"50"`
		ProtectedOnly bool     `json:"protected_only,omitempty"`
		Enabled       bool     `json:"enabled" default:"true"`
		Secret        string   `json:"secret,omitempty" maxLength:"500"`
	}
}

type updateWebhookInput struct {
	Org  string `path:"org"`
	ID   string `path:"id"`
	Body struct {
		URL           string   `json:"url" minLength:"1" maxLength:"2048"`
		Description   string   `json:"description,omitempty" maxLength:"200"`
		Kind          string   `json:"kind,omitempty" enum:"generic,slack,teams,discord"`
		Events        []string `json:"events,omitempty" maxItems:"50"`
		ProtectedOnly bool     `json:"protected_only,omitempty"`
		Enabled       bool     `json:"enabled"`
		Secret        *string  `json:"secret,omitempty" maxLength:"500"`
	}
}

type webhookPath struct {
	Org string `path:"org"`
	ID  string `path:"id"`
}

type webhookDeliveryBody struct {
	ID             string     `json:"id"`
	WebhookID      string     `json:"webhook_id"`
	EventID        string     `json:"event_id"`
	EventType      string     `json:"event_type"`
	URL            string     `json:"url"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus int        `json:"response_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	DeliveredAt    *time.Time `json:"delivered_at,omitempty"`
}

type webhookDeliveriesOutput struct {
	Body struct {
		Deliveries []webhookDeliveryBody `json:"deliveries"`
	}
}

func (s *Server) registerWebhookRoutes() error {
	base := "/api/v1/orgs/{org}/webhooks"
	if err := s.registry.Declare(http.MethodGet, base, authz.SettingsManage, "list outgoing webhooks"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-webhooks", Method: http.MethodGet, Path: base,
		Summary: "List webhooks", Tags: []string{"webhooks"},
	}, func(ctx context.Context, in *orgPath) (*webhooksOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListWebhooks(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &webhooksOutput{}
		out.Body.Webhooks = []webhookBody{}
		for _, view := range views {
			out.Body.Webhooks = append(out.Body.Webhooks, webhookFromView(view))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.SettingsManage, "create outgoing webhooks"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-webhook", Method: http.MethodPost, Path: base,
		Summary: "Create a webhook", Tags: []string{"webhooks"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createWebhookInput) (*webhookOutput, error) {
		actor, _ := actorFrom(ctx)
		secret := in.Body.Secret
		view, err := s.svc.CreateWebhook(ctx, actor, in.Org, service.WebhookInput{
			URL: in.Body.URL, Description: in.Body.Description, Kind: in.Body.Kind,
			Events: in.Body.Events, ProtectedOnly: in.Body.ProtectedOnly,
			Enabled: in.Body.Enabled, Secret: &secret,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &webhookOutput{Body: webhookFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, base+"/{id}", authz.SettingsManage, "update outgoing webhooks"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-webhook", Method: http.MethodPatch, Path: base + "/{id}",
		Summary: "Update a webhook", Tags: []string{"webhooks"},
	}, func(ctx context.Context, in *updateWebhookInput) (*webhookOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.UpdateWebhook(ctx, actor, in.Org, in.ID, service.WebhookInput{
			URL: in.Body.URL, Description: in.Body.Description, Kind: in.Body.Kind,
			Events: in.Body.Events, ProtectedOnly: in.Body.ProtectedOnly,
			Enabled: in.Body.Enabled, Secret: in.Body.Secret,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &webhookOutput{Body: webhookFromView(*view)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.SettingsManage, "delete outgoing webhooks"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-webhook", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a webhook", Tags: []string{"webhooks"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *webhookPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteWebhook(ctx, actor, in.Org, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodGet, base+"/deliveries", authz.SettingsManage, "list webhook delivery attempts"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-webhook-deliveries", Method: http.MethodGet, Path: base + "/deliveries",
		Summary: "List webhook deliveries", Tags: []string{"webhooks"},
	}, func(ctx context.Context, in *orgPath) (*webhookDeliveriesOutput, error) {
		actor, _ := actorFrom(ctx)
		deliveries, err := s.svc.ListWebhookDeliveries(ctx, actor, in.Org, 50)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &webhookDeliveriesOutput{}
		out.Body.Deliveries = []webhookDeliveryBody{}
		for _, delivery := range deliveries {
			out.Body.Deliveries = append(out.Body.Deliveries, webhookDeliveryBody{
				ID: delivery.ID, WebhookID: delivery.WebhookID, EventID: delivery.EventID,
				EventType: delivery.EventType, URL: delivery.URL, Status: delivery.Status,
				Attempts: delivery.Attempts, ResponseStatus: delivery.ResponseStatus,
				LastError: delivery.LastError, CreatedAt: delivery.CreatedAt, DeliveredAt: delivery.DeliveredAt,
			})
		}
		return out, nil
	})
	return nil
}

func webhookFromView(view service.WebhookView) webhookBody {
	return webhookBody{
		ID: view.ID, URL: view.URL, Description: view.Description, Kind: view.Kind,
		Events: view.Events, ProtectedOnly: view.ProtectedOnly, Enabled: view.Enabled,
		SecretSet: view.SecretSet,
	}
}
