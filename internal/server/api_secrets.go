package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type secretPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Key     string `path:"key"`
}

type resolveSecretsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Channel string `query:"channel" enum:"ui,cli,export,api" default:"api"`
	Overlay string `query:"overlay" maxLength:"60"`
}

type overrideInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Key     string `path:"key"`
	Body    struct {
		Value   string `json:"value" maxLength:"1048576"`
		Comment string `json:"comment,omitempty" maxLength:"4096"`
	}
}

type listSecretsInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Reveal  bool   `query:"reveal" doc:"Decrypt values (requires secrets:read-values)"`
	Channel string `query:"channel" enum:"ui,cli,export,api" default:"ui"`
	Overlay string `query:"overlay" maxLength:"60"`
	Reason  string `query:"reason" maxLength:"512" doc:"Why the values are revealed; recorded in the audit log"`
}

type getSecretInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Key     string `path:"key"`
	Reveal  bool   `query:"reveal"`
	Reason  string `query:"reason" maxLength:"1024" doc:"Why the value is being revealed (audited)"`
	Channel string `query:"channel" enum:"ui,cli,export,api" default:"ui"`
	Overlay string `query:"overlay" maxLength:"60"`
}

type createSecretInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Key          string     `json:"key" minLength:"1" maxLength:"256"`
		Value        string     `json:"value" maxLength:"1048576"`
		Kind         string     `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
		Comment      string     `json:"comment,omitempty" maxLength:"4096"`
		Folder       string     `json:"folder,omitempty" maxLength:"512"`
		Notes        string     `json:"notes,omitempty" maxLength:"16384"`
		ProviderURL  string     `json:"provider_url,omitempty" maxLength:"2048"`
		OwnerGroupID string     `json:"owner_group_id,omitempty" maxLength:"60"`
		Tags         []string   `json:"tags,omitempty" maxItems:"20"`
		ExpiresAt    *time.Time `json:"expires_at,omitempty"`
		Message      string     `json:"message,omitempty" maxLength:"512"`
	}
}

type updateSecretInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Key     string `path:"key"`
	Body    struct {
		Value        string     `json:"value" maxLength:"1048576"`
		Kind         string     `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
		Comment      string     `json:"comment,omitempty" maxLength:"4096"`
		Folder       string     `json:"folder,omitempty" maxLength:"512"`
		Notes        string     `json:"notes,omitempty" maxLength:"16384"`
		ProviderURL  string     `json:"provider_url,omitempty" maxLength:"2048"`
		OwnerGroupID string     `json:"owner_group_id,omitempty" maxLength:"60"`
		Tags         []string   `json:"tags,omitempty" maxItems:"20"`
		ExpiresAt    *time.Time `json:"expires_at,omitempty"`
		Message      string     `json:"message,omitempty" maxLength:"512"`
		KeepValue    bool       `json:"keep_value,omitempty" doc:"Keep the current value and ignore value; edits metadata without revealing it"`
	}
}

type secretsOutput struct {
	ETag string `header:"ETag"`
	Body struct {
		Secrets []secretBody `json:"secrets"`
	}
}

type secretOutput struct {
	Body secretBody
}

type batchOutput struct {
	ETag string `header:"ETag"`
	Body struct {
		Created int    `json:"created"`
		Updated int    `json:"updated"`
		Deleted int    `json:"deleted"`
		ETag    string `json:"etag"`
	}
}

type batchEnvResult struct {
	ETag string `json:"etag"`
}

type projectBatchOutput struct {
	Body struct {
		Created      int                       `json:"created"`
		Updated      int                       `json:"updated"`
		Deleted      int                       `json:"deleted"`
		Environments map[string]batchEnvResult `json:"environments"`
	}
}

type secretVersionBody struct {
	Version       int       `json:"version"`
	ChangeMessage string    `json:"change_message,omitempty"`
	ActorID       string    `json:"actor_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

type versionsOutput struct {
	Body struct {
		Versions []secretVersionBody `json:"versions"`
	}
}

type rollbackInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Key     string `path:"key"`
	Body    struct {
		Version int `json:"version" minimum:"1"`
	}
}

func (s *Server) registerSecretRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets", authz.SecretsReadNames, "list secret key names"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-secrets", Method: http.MethodGet,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets",
		Summary: "List secrets", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *listSecretsInput) (*secretsOutput, error) {
		actor, _ := actorFrom(ctx)
		meta := metaFrom(ctx)
		meta.Reason = in.Reason
		secrets, etag, err := s.svc.ListSecretsWithOverlay(ctx, actor, in.Org, in.Project, in.Env, in.Overlay, in.Reveal, in.Channel, meta)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &secretsOutput{ETag: etag}
		for i := range secrets {
			out.Body.Secrets = append(out.Body.Secrets, secretFromService(&secrets[i]))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets", authz.SecretsWrite, "create secrets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-secret", Method: http.MethodPost,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets",
		Summary: "Create a secret", Tags: []string{"secrets"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSecretInput) (*secretOutput, error) {
		actor, _ := actorFrom(ctx)
		created, err := s.svc.CreateSecret(ctx, actor, in.Org, in.Project, in.Env, service.SecretInput{
			Key:           in.Body.Key,
			Value:         in.Body.Value,
			Kind:          in.Body.Kind,
			Comment:       in.Body.Comment,
			Folder:        in.Body.Folder,
			Notes:         in.Body.Notes,
			ProviderURL:   in.Body.ProviderURL,
			OwnerGroupID:  in.Body.OwnerGroupID,
			Tags:          in.Body.Tags,
			ExpiresAt:     in.Body.ExpiresAt,
			ChangeMessage: in.Body.Message,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &secretOutput{Body: secretFromService(created)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}", authz.SecretsReadNames, "read one secret; values need secrets:read-values"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-secret", Method: http.MethodGet,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}",
		Summary: "Get a secret", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *getSecretInput) (*secretOutput, error) {
		actor, _ := actorFrom(ctx)
		secret, err := s.svc.GetSecretWithOverlay(ctx, actor, in.Org, in.Project, in.Env, in.Key, in.Overlay, in.Reveal, in.Reason, in.Channel, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &secretOutput{Body: secretFromService(secret)}, nil
	})

	if err := s.registry.Declare(http.MethodPut, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}", authz.SecretsWrite, "update secrets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "update-secret", Method: http.MethodPut,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}",
		Summary: "Update a secret (creates a version)", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *updateSecretInput) (*secretOutput, error) {
		actor, _ := actorFrom(ctx)
		updated, err := s.svc.UpdateSecret(ctx, actor, in.Org, in.Project, in.Env, in.Key, service.SecretInput{
			Value:         in.Body.Value,
			Kind:          in.Body.Kind,
			Comment:       in.Body.Comment,
			Folder:        in.Body.Folder,
			Notes:         in.Body.Notes,
			ProviderURL:   in.Body.ProviderURL,
			OwnerGroupID:  in.Body.OwnerGroupID,
			Tags:          in.Body.Tags,
			ExpiresAt:     in.Body.ExpiresAt,
			ChangeMessage: in.Body.Message,
			KeepValue:     in.Body.KeepValue,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &secretOutput{Body: secretFromService(updated)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}", authz.SecretsDelete, "delete secrets"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-secret", Method: http.MethodDelete,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}",
		Summary: "Delete a secret (soft delete)", Tags: []string{"secrets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *secretPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSecret(ctx, actor, in.Org, in.Project, in.Env, in.Key, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/versions", authz.SecretsReadNames, "list secret version metadata"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-secret-versions", Method: http.MethodGet,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/versions",
		Summary: "List secret history", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *secretPath) (*versionsOutput, error) {
		actor, _ := actorFrom(ctx)
		versions, err := s.svc.ListSecretVersions(ctx, actor, in.Org, in.Project, in.Env, in.Key)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &versionsOutput{}
		for _, v := range versions {
			out.Body.Versions = append(out.Body.Versions, secretVersionBody{
				Version: v.Version, ChangeMessage: v.ChangeMessage, ActorID: v.ActorID, CreatedAt: v.CreatedAt,
			})
		}
		return out, nil
	})

	type versionValueInput struct {
		Org     string `path:"org"`
		Project string `path:"project"`
		Env     string `path:"env"`
		Key     string `path:"key"`
		Version int    `path:"version" minimum:"1"`
		Reason  string `query:"reason" maxLength:"512"`
	}
	type versionValueOutput struct {
		Body struct {
			Version int    `json:"version"`
			Value   string `json:"value"`
		}
	}
	versionValuePath := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/versions/{version}"
	if err := s.registry.Declare(http.MethodGet, versionValuePath, authz.SecretsReadValues, "preview an old version before restoring it"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-secret-version", Method: http.MethodGet, Path: versionValuePath,
		Summary: "Reveal one past version", Tags: []string{"secrets"},
		Description: "Returns the value of a past version. Audited as secret.reveal; production needs a reason.",
	}, func(ctx context.Context, in *versionValueInput) (*versionValueOutput, error) {
		actor, _ := actorFrom(ctx)
		value, err := s.svc.RevealSecretVersion(ctx, actor, in.Org, in.Project, in.Env, in.Key, in.Version, in.Reason, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &versionValueOutput{}
		out.Body.Version = in.Version
		out.Body.Value = value
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/rollback", authz.SecretsWrite, "roll secrets back to an old version"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "rollback-secret", Method: http.MethodPost,
		Path:    "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/rollback",
		Summary: "Roll a secret back", Tags: []string{"secrets"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *rollbackInput) (*secretOutput, error) {
		actor, _ := actorFrom(ctx)
		secret, err := s.svc.RollbackSecret(ctx, actor, in.Org, in.Project, in.Env, in.Key, in.Body.Version, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &secretOutput{Body: secretFromService(secret)}, nil
	})

	type batchChange struct {
		Key          string   `json:"key" minLength:"1" maxLength:"256"`
		Value        string   `json:"value" maxLength:"1048576"`
		Kind         string   `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
		Comment      string   `json:"comment,omitempty" maxLength:"4096"`
		Folder       string   `json:"folder,omitempty" maxLength:"512"`
		Notes        string   `json:"notes,omitempty" maxLength:"16384"`
		ProviderURL  string   `json:"provider_url,omitempty" maxLength:"2048"`
		OwnerGroupID string   `json:"owner_group_id,omitempty" maxLength:"60"`
		Tags         []string `json:"tags,omitempty" maxItems:"20"`
		Message      string   `json:"message,omitempty" maxLength:"512"`
	}
	type batchInput struct {
		Org     string `path:"org"`
		Project string `path:"project"`
		Env     string `path:"env"`
		IfMatch string `header:"If-Match"`
		Body    struct {
			Changes []batchChange `json:"changes,omitempty" maxItems:"1000"`
			Deletes []string      `json:"deletes,omitempty" maxItems:"1000"`
			Message string        `json:"message,omitempty" maxLength:"512"`
		}
	}
	type projectBatchChange struct {
		Environment string   `json:"environment" minLength:"1" maxLength:"60"`
		Key         string   `json:"key" minLength:"1" maxLength:"256"`
		Value       string   `json:"value" maxLength:"1048576"`
		Kind        string   `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
		Comment     string   `json:"comment,omitempty" maxLength:"4096"`
		Tags        []string `json:"tags,omitempty" maxItems:"20"`
		Message     string   `json:"message,omitempty" maxLength:"512"`
	}
	type projectBatchDelete struct {
		Environment string `json:"environment" minLength:"1" maxLength:"60"`
		Key         string `json:"key" minLength:"1" maxLength:"256"`
	}
	type projectBatchInput struct {
		Org     string `path:"org"`
		Project string `path:"project"`
		Body    struct {
			Changes []projectBatchChange `json:"changes,omitempty" maxItems:"1000"`
			Deletes []projectBatchDelete `json:"deletes,omitempty" maxItems:"1000"`
			IfMatch map[string]string    `json:"if_match"`
			Message string               `json:"message,omitempty" maxLength:"512"`
		}
	}
	projectBatchPath := "/api/v1/orgs/{org}/projects/{project}/secrets:batch"
	if err := s.registry.Declare(http.MethodPatch, projectBatchPath, authz.SecretsWrite, "atomic cross-environment secret edits"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "batch-project-secrets", Method: http.MethodPatch, Path: projectBatchPath,
		Summary: "Apply atomic changes across environments", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *projectBatchInput) (*projectBatchOutput, error) {
		actor, _ := actorFrom(ctx)
		changes := make([]service.EnvSecretChange, 0, len(in.Body.Changes))
		for _, change := range in.Body.Changes {
			changes = append(changes, service.EnvSecretChange{
				Environment: change.Environment,
				Input: service.SecretInput{
					Key: change.Key, Value: change.Value, Kind: change.Kind,
					Comment: change.Comment, Tags: change.Tags, ChangeMessage: change.Message,
				},
			})
		}
		deletes := make([]service.EnvSecretDelete, 0, len(in.Body.Deletes))
		for _, del := range in.Body.Deletes {
			deletes = append(deletes, service.EnvSecretDelete{Environment: del.Environment, Key: del.Key})
		}
		result, err := s.svc.BatchProjectSecrets(ctx, actor, in.Org, in.Project, in.Body.IfMatch, changes, deletes, in.Body.Message, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &projectBatchOutput{}
		out.Body.Created = result.Created
		out.Body.Updated = result.Updated
		out.Body.Deleted = result.Deleted
		out.Body.Environments = make(map[string]batchEnvResult, len(result.Environments))
		for slug, env := range result.Environments {
			out.Body.Environments[slug] = batchEnvResult{ETag: env.ETag}
		}
		return out, nil
	})

	batchPath := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:batch"
	if err := s.registry.Declare(http.MethodPatch, batchPath, authz.SecretsWrite, "atomic multi-key secret edits"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "batch-secrets", Method: http.MethodPatch, Path: batchPath,
		Summary: "Apply atomic secret changes", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *batchInput) (*batchOutput, error) {
		actor, _ := actorFrom(ctx)
		changes := make([]service.SecretInput, 0, len(in.Body.Changes))
		for _, change := range in.Body.Changes {
			changes = append(changes, service.SecretInput{
				Key: change.Key, Value: change.Value, Kind: change.Kind,
				Comment: change.Comment, Tags: change.Tags, ChangeMessage: change.Message,
			})
		}
		result, err := s.svc.BatchSecrets(ctx, actor, in.Org, in.Project, in.Env, in.IfMatch, service.BatchInput{
			Changes: changes, Deletes: in.Body.Deletes, Message: in.Body.Message,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &batchOutput{ETag: result.ETag}
		out.Body.Created = result.Created
		out.Body.Updated = result.Updated
		out.Body.Deleted = result.Deleted
		out.Body.ETag = result.ETag
		return out, nil
	})

	type compareEntry struct {
		Key   string `json:"key" minLength:"1" maxLength:"256"`
		Value string `json:"value" maxLength:"1048576"`
	}
	type compareInput struct {
		Org     string `path:"org"`
		Project string `path:"project"`
		Env     string `path:"env"`
		Body    struct {
			Entries []compareEntry `json:"entries" maxItems:"1000"`
		}
	}
	type compareOutput struct {
		Body struct {
			Statuses map[string]string `json:"statuses" doc:"Per key: new, changed or same. Values are never returned."`
		}
	}
	comparePath := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:compare"
	if err := s.registry.Declare(http.MethodPost, comparePath, authz.SecretsReadValues, "compare values for an import preview; an equality check reads the value"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "compare-secrets", Method: http.MethodPost, Path: comparePath,
		Summary: "Compare values with the stored ones", Tags: []string{"secrets"},
		Description: "Reports per key whether the given value is new, changed or the same as the environment's current value. Values are never returned. Audited as secret.compare.",
	}, func(ctx context.Context, in *compareInput) (*compareOutput, error) {
		actor, _ := actorFrom(ctx)
		values := make(map[string]string, len(in.Body.Entries))
		for _, entry := range in.Body.Entries {
			values[entry.Key] = entry.Value
		}
		statuses, err := s.svc.CompareSecretValues(ctx, actor, in.Org, in.Project, in.Env, values, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &compareOutput{}
		out.Body.Statuses = statuses
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPut, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/override", authz.SecretsWrite, "set personal secret overrides"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "set-secret-override", Method: http.MethodPut, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/override",
		Summary: "Set a personal override", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *overrideInput) (*secretOutput, error) {
		actor, _ := actorFrom(ctx)
		secret, err := s.svc.SetOverride(ctx, actor, in.Org, in.Project, in.Env, in.Key, service.SecretInput{
			Value: in.Body.Value, Comment: in.Body.Comment,
		}, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &secretOutput{Body: secretFromService(secret)}, nil
	})

	if err := s.registry.Declare(http.MethodDelete, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/override", authz.SecretsWrite, "remove personal secret overrides"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-secret-override", Method: http.MethodDelete, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets/{key}/override",
		Summary: "Remove a personal override", Tags: []string{"secrets"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *secretPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteOverride(ctx, actor, in.Org, in.Project, in.Env, in.Key, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:resolve", authz.SecretsReadValues, "resolve secret references"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "resolve-secrets", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:resolve",
		Summary: "Resolve ${KEY} references", Tags: []string{"secrets"},
	}, func(ctx context.Context, in *resolveSecretsInput) (*secretsOutput, error) {
		actor, _ := actorFrom(ctx)
		secrets, etag, err := s.svc.ResolveSecretsWithOverlay(ctx, actor, in.Org, in.Project, in.Env, in.Overlay, in.Channel, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &secretsOutput{ETag: etag}
		for i := range secrets {
			out.Body.Secrets = append(out.Body.Secrets, secretFromService(&secrets[i]))
		}
		return out, nil
	})

	return nil
}
