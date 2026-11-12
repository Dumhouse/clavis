package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type overlayPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
}

type overlaySecretPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Overlay string `path:"overlay"`
	Key     string `path:"key"`
}

type createOverlayInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Slug string `json:"slug" minLength:"1" maxLength:"60"`
		Name string `json:"name,omitempty" maxLength:"100"`
	}
}

type overlayBody struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type overlaysOutput struct {
	Body struct {
		Overlays []overlayBody `json:"overlays"`
	}
}

type overlayOutput struct {
	Body overlayBody
}

type overlaySecretsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Overlay string `path:"overlay"`
	Reveal  bool   `query:"reveal"`
}

type overlaySecretBody struct {
	Key       string `json:"key"`
	Kind      string `json:"kind"`
	Value     string `json:"value,omitempty"`
	Version   int    `json:"version"`
	Revealed  bool   `json:"revealed"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

type overlaySecretsOutput struct {
	Body struct {
		Secrets []overlaySecretBody `json:"secrets"`
	}
}

type overlaySecretInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Overlay string `path:"overlay"`
	Key     string `path:"key"`
	Body    struct {
		Value string `json:"value" maxLength:"1048576"`
		Kind  string `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
	}
}

func (s *Server) registerOverlayRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/overlays"
	if err := s.registry.Declare(http.MethodGet, base, authz.SecretsReadNames, "list tenant/region overlays"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-overlays", Method: http.MethodGet, Path: base,
		Summary: "List tenant/region overlays", Tags: []string{"overlays"},
	}, func(ctx context.Context, in *overlayPath) (*overlaysOutput, error) {
		actor, _ := actorFrom(ctx)
		overlays, err := s.svc.ListEnvironmentOverlays(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &overlaysOutput{}
		for _, overlay := range overlays {
			out.Body.Overlays = append(out.Body.Overlays, overlayBody{
				ID: overlay.ID, Slug: overlay.Slug, Name: overlay.Name,
				CreatedAt: overlay.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.EnvironmentsManage, "create tenant/region overlays"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-overlay", Method: http.MethodPost, Path: base,
		Summary: "Create a tenant/region overlay", Tags: []string{"overlays"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createOverlayInput) (*overlayOutput, error) {
		actor, _ := actorFrom(ctx)
		overlay, err := s.svc.CreateEnvironmentOverlay(ctx, actor, in.Org, in.Project, in.Env, in.Body.Slug, in.Body.Name, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &overlayOutput{Body: overlayBody{
			ID: overlay.ID, Slug: overlay.Slug, Name: overlay.Name,
			CreatedAt: overlay.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		}}, nil
	})

	secretsPath := base + "/{overlay}/secrets"
	if err := s.registry.Declare(http.MethodGet, secretsPath, authz.SecretsReadNames, "list overlay values"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-overlay-secrets", Method: http.MethodGet, Path: secretsPath,
		Summary: "List overlay values", Tags: []string{"overlays"},
	}, func(ctx context.Context, in *overlaySecretsInput) (*overlaySecretsOutput, error) {
		actor, _ := actorFrom(ctx)
		secrets, err := s.svc.ListOverlaySecrets(ctx, actor, in.Org, in.Project, in.Env, in.Overlay, in.Reveal)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &overlaySecretsOutput{}
		for _, secret := range secrets {
			out.Body.Secrets = append(out.Body.Secrets, overlaySecretBody{
				Key: secret.Key, Kind: secret.Kind, Value: secret.Value, Version: secret.Version,
				Revealed: secret.Revealed, UpdatedAt: secret.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		return out, nil
	})

	secretPath := secretsPath + "/{key}"
	if err := s.registry.Declare(http.MethodPut, secretPath, authz.SecretsWrite, "set overlay values"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "set-overlay-secret", Method: http.MethodPut, Path: secretPath,
		Summary: "Set an overlay value", Tags: []string{"overlays"},
	}, func(ctx context.Context, in *overlaySecretInput) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.SetOverlayValue(ctx, actor, in.Org, in.Project, in.Env, in.Overlay, in.Key,
			in.Body.Value, in.Body.Kind, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	if err := s.registry.Declare(http.MethodDelete, secretPath, authz.SecretsDelete, "delete overlay values"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-overlay-secret", Method: http.MethodDelete, Path: secretPath,
		Summary: "Delete an overlay value", Tags: []string{"overlays"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *overlaySecretPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteOverlaySecret(ctx, actor, in.Org, in.Project, in.Env, in.Overlay, in.Key, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}
