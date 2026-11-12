package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/pkg/problem"
)

type orgPath struct {
	Org    string `path:"org" doc:"Organization slug"`
	Reauth string `header:"X-Clavis-Reauth" doc:"Short-lived token from /auth/reauthenticate"`
}

type projectPath struct {
	Org     string `path:"org" doc:"Organization slug"`
	Project string `path:"project" doc:"Project slug"`
	Reauth  string `header:"X-Clavis-Reauth" doc:"Short-lived token from /auth/reauthenticate"`
}

type environmentPath struct {
	Org     string `path:"org" doc:"Organization slug"`
	Project string `path:"project" doc:"Project slug"`
	Env     string `path:"env" doc:"Environment slug"`
}

type listOrgsOutput struct {
	Body struct {
		Orgs []orgBody `json:"orgs"`
	}
}

type createOrgInput struct {
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
	}
}

type orgOutput struct {
	Body orgBody
}

type projectsOutput struct {
	Body struct {
		Projects []projectBody `json:"projects"`
	}
}

type createProjectInput struct {
	Org  string `path:"org"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
	}
}

type projectOutput struct {
	Body projectBody
}

type createEnvironmentInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
		Type string `json:"type" enum:"development,preview,staging,production"`
	}
}

type updateEnvironmentInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Name       *string `json:"name,omitempty" maxLength:"100"`
		Type       *string `json:"type,omitempty" enum:"development,preview,staging,production"`
		Protected  *bool   `json:"protected,omitempty"`
		Position   *int    `json:"position,omitempty"`
		ParentSlug *string `json:"parent_slug,omitempty" maxLength:"60"`
	}
}

type environmentOutput struct {
	Body envBody
}

type cloneEnvironmentInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Slug          string `json:"slug" minLength:"1" maxLength:"60"`
		Name          string `json:"name" minLength:"1" maxLength:"100"`
		IncludeValues bool   `json:"include_values,omitempty"`
	}
}

type orgSettingsInput struct {
	Org  string `path:"org"`
	Body struct {
		DefaultAppearance    *string                `json:"default_appearance,omitempty" enum:"classic,warm"`
		RequireMFA           *bool                  `json:"require_mfa,omitempty"`
		SessionIdleHours     *int                   `json:"session_idle_hours,omitempty" minimum:"0" maximum:"168"`
		SessionAbsoluteDays  *int                   `json:"session_absolute_days,omitempty" minimum:"0" maximum:"365"`
		IPAllowlist          *[]string              `json:"ip_allowlist,omitempty" maxItems:"100"`
		BlockCLIWrites       *bool                  `json:"block_cli_writes,omitempty"`
		EnvironmentTemplates *[]environmentTemplate `json:"environment_templates,omitempty" maxItems:"20"`
		SSO                  *ssoInput              `json:"sso,omitempty"`
		GitHubWebhookSecret  *string                `json:"github_webhook_secret,omitempty" maxLength:"500"`
		PreviewEnabled       *bool                  `json:"preview_enabled,omitempty"`
		PreviewRetentionDays *int                   `json:"preview_retention_days,omitempty" minimum:"1" maximum:"90"`
		DuplicateDetection   *bool                  `json:"duplicate_detection,omitempty"`
	}
}

type ssoInput struct {
	Enabled        *bool     `json:"enabled,omitempty"`
	Provider       *string   `json:"provider,omitempty" enum:"google,github,microsoft,okta,generic,saml"`
	ClientID       *string   `json:"client_id,omitempty" maxLength:"500"`
	ClientSecret   *string   `json:"client_secret,omitempty" maxLength:"4096"`
	Issuer         *string   `json:"issuer,omitempty" maxLength:"500"`
	Enforce        *bool     `json:"enforce,omitempty"`
	AllowedDomains *[]string `json:"allowed_domains,omitempty" maxItems:"50"`
	Role           *string   `json:"role,omitempty" maxLength:"60"`
	IdPMetadata    *string   `json:"idp_metadata,omitempty" maxLength:"1048576"`
}

type ssoBody struct {
	Enabled         bool     `json:"enabled"`
	Provider        string   `json:"provider"`
	ClientID        string   `json:"client_id"`
	ClientSecretSet bool     `json:"client_secret_set"`
	Issuer          string   `json:"issuer"`
	Enforce         bool     `json:"enforce"`
	AllowedDomains  []string `json:"allowed_domains"`
	Role            string   `json:"role"`
	IdPMetadata     string   `json:"idp_metadata,omitempty"`
}

type environmentTemplate struct {
	Slug      string `json:"slug" minLength:"1" maxLength:"60"`
	Name      string `json:"name" minLength:"1" maxLength:"100"`
	Type      string `json:"type" enum:"development,preview,staging,production"`
	Color     string `json:"color,omitempty" maxLength:"20"`
	Protected bool   `json:"protected,omitempty"`
}

type settingsBody struct {
	DefaultAppearance      string                `json:"default_appearance"`
	RequireMFA             bool                  `json:"require_mfa"`
	SessionIdleHours       int                   `json:"session_idle_hours"`
	SessionAbsoluteDays    int                   `json:"session_absolute_days"`
	IPAllowlist            []string              `json:"ip_allowlist"`
	BlockCLIWrites         bool                  `json:"block_cli_writes"`
	EnvironmentTemplates   []environmentTemplate `json:"environment_templates"`
	SSO                    ssoBody               `json:"sso"`
	GitHubWebhookSecretSet bool                  `json:"github_webhook_secret_set"`
	PreviewEnabled         bool                  `json:"preview_enabled"`
	PreviewRetentionDays   int                   `json:"preview_retention_days"`
	DuplicateDetection     bool                  `json:"duplicate_detection"`
}

type orgSettingsOutput struct {
	Body settingsBody
}

func settingsBodyFromService(settings service.OrgSettings) settingsBody {
	appearance := settings.DefaultAppearance
	if appearance == "" {
		appearance = "classic"
	}
	allowlist := settings.IPAllowlist
	if allowlist == nil {
		allowlist = []string{}
	}
	templates := make([]environmentTemplate, 0, len(settings.EnvironmentTemplates))
	for _, template := range settings.EnvironmentTemplates {
		templates = append(templates, environmentTemplate{
			Slug: template.Slug, Name: template.Name, Type: template.Type,
			Color: template.Color, Protected: template.Protected,
		})
	}
	return settingsBody{
		DefaultAppearance: appearance, RequireMFA: settings.RequireMFA,
		SessionIdleHours: settings.SessionIdleHours, SessionAbsoluteDays: settings.SessionAbsoluteDays,
		IPAllowlist: allowlist, BlockCLIWrites: settings.BlockCLIWrites,
		EnvironmentTemplates:   templates,
		GitHubWebhookSecretSet: len(settings.GitHubWebhookSecret) > 0,
		PreviewEnabled:         settings.PreviewEnabled,
		PreviewRetentionDays:   settings.PreviewRetentionDays,
		DuplicateDetection:     settings.DuplicateDetection,
		SSO: ssoBody{
			Enabled: settings.SSOEnabled, Provider: settings.SSOProvider, ClientID: settings.SSOClientID,
			ClientSecretSet: len(settings.SSOClientSecret) > 0, Issuer: settings.SSOIssuer,
			Enforce: settings.SSOEnforce, AllowedDomains: settings.SSOAllowedDomains, Role: settings.SSORole,
			IdPMetadata: settings.SSOIdPMetadata,
		},
	}
}

type updateOrgInput struct {
	Org  string `path:"org"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
	}
}

type updateProjectInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Name string `json:"name" minLength:"1" maxLength:"100"`
		Slug string `json:"slug,omitempty" maxLength:"60"`
	}
}

func (s *Server) registerOrgRoutes() error {
	if err := s.registry.DeclareAuthenticated(http.MethodGet, "/api/v1/orgs", "lists organizations the caller belongs to"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-orgs", Method: http.MethodGet, Path: "/api/v1/orgs",
		Summary: "List organizations", Tags: []string{"orgs"},
	}, func(ctx context.Context, _ *struct{}) (*listOrgsOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(problem.Unauthorized("Sign in to continue."), ctx)
		}
		memberships, err := s.svc.ListOrgs(ctx, principal.User.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &listOrgsOutput{}
		for _, m := range memberships {
			out.Body.Orgs = append(out.Body.Orgs, orgFromStore(&m.Org, m.RoleName))
		}
		return out, nil
	})

	if err := s.registry.DeclareAuthenticated(http.MethodPost, "/api/v1/orgs", "any signed-in user may create a new organization"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-org", Method: http.MethodPost, Path: "/api/v1/orgs",
		Summary: "Create an organization", Tags: []string{"orgs"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createOrgInput) (*orgOutput, error) {
		principal, ok := principalFrom(ctx)
		if !ok {
			return nil, withRequestID(problem.Unauthorized("Sign in to continue."), ctx)
		}
		org, err := s.svc.CreateOrg(ctx, principal.User, in.Body.Name, in.Body.Slug)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &orgOutput{Body: orgFromStore(org, authz.RoleOwner)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}", authz.OrgRead, "read organization metadata"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-org", Method: http.MethodGet, Path: "/api/v1/orgs/{org}",
		Summary: "Get an organization", Tags: []string{"orgs"},
	}, func(ctx context.Context, in *orgPath) (*orgOutput, error) {
		actor, _ := actorFrom(ctx)
		org, role, err := s.svc.GetOrg(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &orgOutput{Body: orgFromStore(org, role)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects", authz.ProjectsRead, "list projects"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-projects", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/projects",
		Summary: "List projects", Tags: []string{"projects"},
	}, func(ctx context.Context, in *orgPath) (*projectsOutput, error) {
		actor, _ := actorFrom(ctx)
		projects, err := s.svc.ListProjects(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &projectsOutput{}
		for i := range projects {
			envs, err := s.store.ListEnvironments(ctx, projects[i].ID)
			if err != nil {
				return nil, s.apiError(ctx, err)
			}
			out.Body.Projects = append(out.Body.Projects, projectFromStore(&projects[i], envs))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/projects", authz.ProjectsManage, "create projects"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-project", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/projects",
		Summary: "Create a project with default environments", Tags: []string{"projects"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createProjectInput) (*projectOutput, error) {
		actor, _ := actorFrom(ctx)
		project, envs, err := s.svc.CreateProject(ctx, actor, in.Org, in.Body.Name, in.Body.Slug)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &projectOutput{Body: projectFromStore(project, envs)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects/{project}", authz.ProjectsRead, "read a project and its environments"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-project", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/projects/{project}",
		Summary: "Get a project", Tags: []string{"projects"},
	}, func(ctx context.Context, in *projectPath) (*projectOutput, error) {
		actor, _ := actorFrom(ctx)
		project, envs, err := s.svc.GetProject(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &projectOutput{Body: projectFromStore(project, envs)}, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/projects/{project}/environments", authz.EnvironmentsManage, "create environments"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-environment", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/projects/{project}/environments",
		Summary: "Create an environment", Tags: []string{"environments"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createEnvironmentInput) (*environmentOutput, error) {
		actor, _ := actorFrom(ctx)
		env, err := s.svc.CreateEnvironment(ctx, actor, in.Org, in.Project, in.Body.Name, in.Body.Slug, in.Body.Type)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &environmentOutput{Body: envFromStore(env)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, "/api/v1/orgs/{org}/projects/{project}/environments/{env}", authz.EnvironmentsManage, "update environments"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-environment", Method: http.MethodPatch, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}",
		Summary: "Update an environment", Tags: []string{"environments"},
	}, func(ctx context.Context, in *updateEnvironmentInput) (*environmentOutput, error) {
		actor, _ := actorFrom(ctx)
		patch := service.EnvironmentPatch{
			Name: in.Body.Name, Type: in.Body.Type, Protected: in.Body.Protected,
			Position: in.Body.Position, ParentSlug: in.Body.ParentSlug,
		}
		env, err := s.svc.UpdateEnvironment(ctx, actor, in.Org, in.Project, in.Env, patch)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &environmentOutput{Body: envFromStore(env)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/settings", authz.SettingsManage, "read organization security settings"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-org-settings", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/settings",
		Summary: "Read organization settings", Tags: []string{"orgs"},
	}, func(ctx context.Context, in *orgPath) (*orgSettingsOutput, error) {
		actor, _ := actorFrom(ctx)
		org, _, err := s.svc.GetOrg(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		settings := service.ParseOrgSettings(org.Settings)
		return &orgSettingsOutput{Body: settingsBodyFromService(settings)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, "/api/v1/orgs/{org}/settings", authz.SettingsManage, "update organization security settings"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-org-settings", Method: http.MethodPatch, Path: "/api/v1/orgs/{org}/settings",
		Summary: "Update organization settings", Tags: []string{"orgs"},
	}, func(ctx context.Context, in *orgSettingsInput) (*orgSettingsOutput, error) {
		actor, _ := actorFrom(ctx)
		patch := service.OrgSettingsPatch{
			DefaultAppearance: in.Body.DefaultAppearance, RequireMFA: in.Body.RequireMFA,
			SessionIdleHours: in.Body.SessionIdleHours, SessionAbsoluteDays: in.Body.SessionAbsoluteDays,
			IPAllowlist: in.Body.IPAllowlist, BlockCLIWrites: in.Body.BlockCLIWrites,
			GitHubWebhookSecret: in.Body.GitHubWebhookSecret, PreviewEnabled: in.Body.PreviewEnabled,
			PreviewRetentionDays: in.Body.PreviewRetentionDays,
			DuplicateDetection:   in.Body.DuplicateDetection,
		}
		if in.Body.SSO != nil {
			patch.SSOEnabled = in.Body.SSO.Enabled
			patch.SSOProvider = in.Body.SSO.Provider
			patch.SSOClientID = in.Body.SSO.ClientID
			patch.SSOClientSecret = in.Body.SSO.ClientSecret
			patch.SSOIssuer = in.Body.SSO.Issuer
			patch.SSOEnforce = in.Body.SSO.Enforce
			patch.SSOAllowedDomains = in.Body.SSO.AllowedDomains
			patch.SSORole = in.Body.SSO.Role
			patch.SSOIdPMetadata = in.Body.SSO.IdPMetadata
		}
		var templates *[]service.EnvironmentTemplate
		if in.Body.EnvironmentTemplates != nil {
			converted := make([]service.EnvironmentTemplate, 0, len(*in.Body.EnvironmentTemplates))
			for _, template := range *in.Body.EnvironmentTemplates {
				converted = append(converted, service.EnvironmentTemplate{
					Slug: template.Slug, Name: template.Name, Type: template.Type,
					Color: template.Color, Protected: template.Protected,
				})
			}
			templates = &converted
		}
		patch.EnvironmentTemplates = templates
		_, settings, err := s.svc.UpdateOrgSettings(ctx, actor, in.Org, patch, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &orgSettingsOutput{Body: settingsBodyFromService(settings)}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, "/api/v1/orgs/{org}", authz.SettingsManage, "update organization settings"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-org", Method: http.MethodPatch, Path: "/api/v1/orgs/{org}",
		Summary: "Update an organization", Tags: []string{"orgs"},
	}, func(ctx context.Context, in *updateOrgInput) (*orgOutput, error) {
		actor, _ := actorFrom(ctx)
		org, err := s.svc.UpdateOrg(ctx, actor, in.Org, in.Body.Name, in.Body.Slug, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &orgOutput{Body: orgFromStore(org, "")}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}", authz.OrgDelete, "delete organizations"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-org", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}",
		Summary: "Delete an organization", Tags: []string{"orgs"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *orgPath) (*emptyOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteOrg(ctx, actor, in.Org, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPatch, "/api/v1/orgs/{org}/projects/{project}", authz.ProjectsManage, "update projects"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-project", Method: http.MethodPatch, Path: "/api/v1/orgs/{org}/projects/{project}",
		Summary: "Update a project", Tags: []string{"projects"},
	}, func(ctx context.Context, in *updateProjectInput) (*projectOutput, error) {
		actor, _ := actorFrom(ctx)
		project, err := s.svc.UpdateProject(ctx, actor, in.Org, in.Project, in.Body.Name, in.Body.Slug, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		envs, err := s.store.ListEnvironments(ctx, project.ID)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &projectOutput{Body: projectFromStore(project, envs)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/projects/{project}", authz.ProjectsManage, "delete projects"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-project", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/projects/{project}",
		Summary: "Delete a project", Tags: []string{"projects"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *projectPath) (*emptyOutput, error) {
		if err := s.requireReauth(ctx, in.Reauth); err != nil {
			return nil, err
		}
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteProject(ctx, actor, in.Org, in.Project, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/clone", authz.EnvironmentsManage, "clone environments"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "clone-environment", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}/clone",
		Summary: "Clone an environment", Tags: []string{"environments"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *cloneEnvironmentInput) (*environmentOutput, error) {
		actor, _ := actorFrom(ctx)
		env, err := s.svc.CloneEnvironment(ctx, actor, in.Org, in.Project, in.Env, service.CloneEnvironmentInput{
			Slug: in.Body.Slug, Name: in.Body.Name, IncludeValues: in.Body.IncludeValues,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &environmentOutput{Body: envFromStore(env)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/projects/{project}/environments/{env}", authz.EnvironmentsManage, "delete environments"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-environment", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}",
		Summary: "Delete an environment", Tags: []string{"environments"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *environmentPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteEnvironment(ctx, actor, in.Org, in.Project, in.Env); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	return nil
}
