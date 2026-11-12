package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type dynamicConfigPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

type createDynamicConfigInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Engine     string         `json:"engine" enum:"postgres,aws"`
		Config     map[string]any `json:"config"`
		DefaultTTL int            `json:"default_ttl_seconds,omitempty" minimum:"60" maximum:"604800"`
		MaxTTL     int            `json:"max_ttl_seconds,omitempty" minimum:"60" maximum:"604800"`
	}
}

type dynamicConfigBody struct {
	ID         string    `json:"id"`
	Engine     string    `json:"engine"`
	DefaultTTL int       `json:"default_ttl_seconds"`
	MaxTTL     int       `json:"max_ttl_seconds"`
	CreatedAt  time.Time `json:"created_at"`
}

type dynamicConfigsOutput struct {
	Body struct {
		Configs []dynamicConfigBody `json:"configs"`
	}
}

type dynamicConfigOutput struct {
	Body dynamicConfigBody
}

type leasePath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
}

type createLeaseInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
	Body    struct {
		TTLSeconds int `json:"ttl_seconds,omitempty" minimum:"60" maximum:"604800"`
	}
}

type renewLeaseInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	ID      string `path:"id"`
	Body    struct {
		TTLSeconds int `json:"ttl_seconds,omitempty" minimum:"60" maximum:"604800"`
	}
}

type leaseBody struct {
	ID         string     `json:"id"`
	ConfigID   string     `json:"config_id"`
	IdentityID string     `json:"identity_id,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type leaseOutput struct {
	Body struct {
		Lease   leaseBody         `json:"lease"`
		Secrets map[string]string `json:"secrets"`
	}
}

type leasesOutput struct {
	Body struct {
		Leases []leaseBody `json:"leases"`
	}
}

func (s *Server) registerDynamicRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/dynamic-configs"
	if err := s.registry.Declare(http.MethodGet, base, authz.ProjectsRead, "list dynamic secret configs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-dynamic-configs", Method: http.MethodGet, Path: base,
		Summary: "List dynamic secret configs", Tags: []string{"dynamic"},
	}, func(ctx context.Context, in *projectPath) (*dynamicConfigsOutput, error) {
		actor, _ := actorFrom(ctx)
		configs, err := s.svc.ListDynamicConfigs(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &dynamicConfigsOutput{}
		for _, config := range configs {
			out.Body.Configs = append(out.Body.Configs, dynamicConfigBody{
				ID: config.ID, Engine: config.Engine, DefaultTTL: config.DefaultTTL,
				MaxTTL: config.MaxTTL, CreatedAt: config.CreatedAt,
			})
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, base, authz.ProjectsManage, "create dynamic secret configs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-dynamic-config", Method: http.MethodPost, Path: base,
		Summary: "Create a dynamic secret config", Tags: []string{"dynamic"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createDynamicConfigInput) (*dynamicConfigOutput, error) {
		actor, _ := actorFrom(ctx)
		config, err := s.svc.CreateDynamicConfig(ctx, actor, in.Org, in.Project, in.Body.Engine,
			in.Body.Config, in.Body.DefaultTTL, in.Body.MaxTTL, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &dynamicConfigOutput{Body: dynamicConfigBody{
			ID: config.ID, Engine: config.Engine, DefaultTTL: config.DefaultTTL,
			MaxTTL: config.MaxTTL, CreatedAt: config.CreatedAt,
		}}, nil
	})
	if err := s.registry.Declare(http.MethodDelete, base+"/{id}", authz.ProjectsManage, "delete dynamic secret configs"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-dynamic-config", Method: http.MethodDelete, Path: base + "/{id}",
		Summary: "Delete a dynamic secret config", Tags: []string{"dynamic"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *dynamicConfigPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteDynamicConfig(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	leasesPath := base + "/{id}/leases"
	if err := s.registry.Declare(http.MethodGet, leasesPath, authz.ProjectsRead, "list leases"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-leases", Method: http.MethodGet, Path: leasesPath,
		Summary: "List leases", Tags: []string{"dynamic"},
	}, func(ctx context.Context, in *dynamicConfigPath) (*leasesOutput, error) {
		actor, _ := actorFrom(ctx)
		leases, err := s.svc.ListLeases(ctx, actor, in.Org, in.Project, in.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &leasesOutput{}
		for _, lease := range leases {
			out.Body.Leases = append(out.Body.Leases, leaseFromService(lease))
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, leasesPath, authz.SecretsUse, "create dynamic credentials"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-lease", Method: http.MethodPost, Path: leasesPath,
		Summary: "Create dynamic credentials", Tags: []string{"dynamic"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createLeaseInput) (*leaseOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.CreateLease(ctx, actor, in.Org, in.Project, in.ID, in.Body.TTLSeconds, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &leaseOutput{}
		out.Body.Lease = leaseFromService(result.Lease)
		out.Body.Secrets = result.Data
		return out, nil
	})

	leaseBase := "/api/v1/orgs/{org}/projects/{project}/leases/{id}"
	if err := s.registry.Declare(http.MethodPost, leaseBase+"/renew", authz.SecretsUse, "renew leases"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "renew-lease", Method: http.MethodPost, Path: leaseBase + "/renew",
		Summary: "Renew a lease", Tags: []string{"dynamic"},
	}, func(ctx context.Context, in *renewLeaseInput) (*leaseOutput, error) {
		actor, _ := actorFrom(ctx)
		lease, err := s.svc.RenewLease(ctx, actor, in.Org, in.Project, in.ID, in.Body.TTLSeconds, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &leaseOutput{}
		out.Body.Lease = leaseFromService(*lease)
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, leaseBase+"/revoke", authz.SecretsDelete, "revoke leases"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "revoke-lease", Method: http.MethodPost, Path: leaseBase + "/revoke",
		Summary: "Revoke a lease", Tags: []string{"dynamic"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *leasePath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.RevokeLease(ctx, actor, in.Org, in.Project, in.ID, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}

func leaseFromService(lease service.LeaseView) leaseBody {
	return leaseBody{
		ID: lease.ID, ConfigID: lease.ConfigID, IdentityID: lease.IdentityID,
		ExpiresAt: lease.ExpiresAt, RevokedAt: lease.RevokedAt, CreatedAt: lease.CreatedAt,
	}
}
