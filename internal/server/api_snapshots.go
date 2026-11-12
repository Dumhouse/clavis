package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type snapshotBody struct {
	ID        string    `json:"id"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type snapshotsOutput struct {
	Body struct {
		Snapshots []snapshotBody `json:"snapshots"`
	}
}

type snapshotOutput struct {
	Body snapshotBody
}

type environmentSnapshotPath struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
}

type createSnapshotInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Reason string `json:"reason,omitempty" maxLength:"512"`
	}
}

type restoreSnapshotInput struct {
	Org        string `path:"org"`
	Project    string `path:"project"`
	Env        string `path:"env"`
	SnapshotID string `path:"id"`
}

type restoreSnapshotOutput struct {
	Body struct {
		Restored int `json:"restored"`
	}
}

func (s *Server) registerSnapshotRoutes() error {
	base := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/snapshots"
	if err := s.registry.Declare(http.MethodGet, base, authz.EnvironmentsRead, "list environment snapshots"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-snapshots", Method: http.MethodGet, Path: base,
		Summary: "List environment snapshots", Tags: []string{"snapshots"},
	}, func(ctx context.Context, in *environmentSnapshotPath) (*snapshotsOutput, error) {
		actor, _ := actorFrom(ctx)
		views, err := s.svc.ListSnapshots(ctx, actor, in.Org, in.Project, in.Env)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &snapshotsOutput{}
		for _, view := range views {
			out.Body.Snapshots = append(out.Body.Snapshots, snapshotBody{
				ID: view.ID, Reason: view.Reason, CreatedAt: view.CreatedAt,
			})
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodPost, base, authz.EnvironmentsManage, "capture environment snapshots"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-snapshot", Method: http.MethodPost, Path: base,
		Summary: "Capture an environment snapshot", Tags: []string{"snapshots"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createSnapshotInput) (*snapshotOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.CreateSnapshot(ctx, actor, in.Org, in.Project, in.Env, in.Body.Reason, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &snapshotOutput{Body: snapshotBody{ID: view.ID, Reason: view.Reason, CreatedAt: view.CreatedAt}}, nil
	})

	restorePath := base + "/{id}/restore"
	if err := s.registry.Declare(http.MethodPost, restorePath, authz.SecretsWrite, "restore environment snapshots"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "restore-snapshot", Method: http.MethodPost, Path: restorePath,
		Summary: "Restore an environment snapshot", Tags: []string{"snapshots"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *restoreSnapshotInput) (*restoreSnapshotOutput, error) {
		actor, _ := actorFrom(ctx)
		restored, err := s.svc.RestoreSnapshot(ctx, actor, in.Org, in.Project, in.Env, in.SnapshotID, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &restoreSnapshotOutput{}
		out.Body.Restored = restored
		return out, nil
	})
	return nil
}
