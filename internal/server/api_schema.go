package server

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/service"
)

type projectSchemaPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
}

type projectSchemaKeyPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Key     string `path:"key"`
}

type schemaEntryBody struct {
	Key         string          `json:"key" minLength:"1" maxLength:"256"`
	Type        string          `json:"type,omitempty" enum:"string,url,int,bool,json,enum" default:"string"`
	Kind        string          `json:"kind,omitempty" enum:"secret,variable,file" default:"secret"`
	Required    map[string]bool `json:"required,omitempty" doc:"Whether the key is required, keyed by environment type"`
	Pattern     string          `json:"pattern,omitempty" maxLength:"512"`
	Description string          `json:"description,omitempty" maxLength:"512"`
	Example     string          `json:"example,omitempty" maxLength:"1024"`
	Enum        []string        `json:"enum,omitempty" maxItems:"200"`
}

type schemaOutput struct {
	Body struct {
		Schema []schemaEntryBody `json:"schema"`
	}
}

type replaceSchemaInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Body    struct {
		Schema []schemaEntryBody `json:"schema" maxItems:"1000"`
	}
}

func schemaFromService(entry *service.SchemaEntry) schemaEntryBody {
	return schemaEntryBody{
		Key: entry.Key, Type: entry.Type, Kind: entry.Kind, Required: entry.Required,
		Pattern: entry.Pattern, Description: entry.Description, Example: entry.Example,
		Enum: entry.Enum,
	}
}

func (s *Server) registerSchemaRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/schema"
	if err := s.registry.Declare(http.MethodGet, base, authz.ProjectsRead, "read a project's secret schema"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "get-schema", Method: http.MethodGet, Path: base,
		Summary: "Read a project's secret schema", Tags: []string{"schema"},
	}, func(ctx context.Context, in *projectSchemaPath) (*schemaOutput, error) {
		actor, _ := actorFrom(ctx)
		entries, err := s.svc.ListSchema(ctx, actor, in.Org, in.Project)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &schemaOutput{}
		for i := range entries {
			out.Body.Schema = append(out.Body.Schema, schemaFromService(&entries[i]))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPut, base, authz.ProjectsManage, "replace a project's secret schema"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "replace-schema", Method: http.MethodPut, Path: base,
		Summary: "Replace a project's secret schema", Tags: []string{"schema"},
	}, func(ctx context.Context, in *replaceSchemaInput) (*schemaOutput, error) {
		actor, _ := actorFrom(ctx)
		entries := make([]service.SchemaEntry, 0, len(in.Body.Schema))
		for i := range in.Body.Schema {
			body := in.Body.Schema[i]
			entries = append(entries, service.SchemaEntry{
				Key: body.Key, Type: body.Type, Kind: body.Kind, Required: body.Required,
				Pattern: body.Pattern, Description: body.Description, Example: body.Example,
				Enum: body.Enum,
			})
		}
		updated, err := s.svc.ReplaceSchema(ctx, actor, in.Org, in.Project, entries, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &schemaOutput{}
		for i := range updated {
			out.Body.Schema = append(out.Body.Schema, schemaFromService(&updated[i]))
		}
		return out, nil
	})

	keyPath := base + "/{key}"
	if err := s.registry.Declare(http.MethodDelete, keyPath, authz.ProjectsManage, "delete one schema entry"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "delete-schema-entry", Method: http.MethodDelete, Path: keyPath,
		Summary: "Delete one schema entry", Tags: []string{"schema"}, DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *projectSchemaKeyPath) (*emptyOutput, error) {
		actor, _ := actorFrom(ctx)
		if err := s.svc.DeleteSchemaKey(ctx, actor, in.Org, in.Project, in.Key, metaFrom(ctx)); err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &emptyOutput{}, nil
	})
	return nil
}
