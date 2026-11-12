package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
)

type auditEventBody struct {
	ID        string      `json:"id"`
	Seq       int64       `json:"seq"`
	Actor     auditActor  `json:"actor"`
	Action    string      `json:"action"`
	Resource  auditRes    `json:"resource"`
	Details   auditDetail `json:"details"`
	IP        string      `json:"ip,omitempty"`
	UserAgent string      `json:"user_agent,omitempty"`
	CreatedAt time.Time   `json:"created_at"`
}

type auditActor struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

type auditRes struct {
	Type      string `json:"type"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	EnvID     string `json:"env_id,omitempty"`
	// Labels resolved at read time; never stored in the chain.
	Project     string `json:"project,omitempty"`
	Environment string `json:"environment,omitempty"`
	Key         string `json:"key,omitempty" doc:"Decrypted key name, when the caller may read names"`
}

type auditDetail struct {
	KeyBlindIndex string   `json:"key_blind_index,omitempty"`
	Path          string   `json:"path,omitempty"`
	Environment   string   `json:"environment,omitempty"`
	Version       int      `json:"version,omitempty"`
	Reason        string   `json:"reason,omitempty"`
	Channel       string   `json:"channel,omitempty"`
	Command       string   `json:"command,omitempty"`
	Commands      []string `json:"commands,omitempty"`
	SchemaKey     string   `json:"schema_key,omitempty"`
	RequestedKey  string   `json:"requested_key,omitempty"`
	Count         int      `json:"count,omitempty"`
	RequestID     string   `json:"request_id,omitempty"`
}

type auditInput struct {
	Org      string `path:"org"`
	AfterSeq int64  `query:"after" minimum:"0" default:"0"`
	Limit    int    `query:"limit" minimum:"1" maximum:"200" default:"50"`
}

type auditOutput struct {
	Body struct {
		Events   []auditEventBody `json:"events"`
		Verified bool             `json:"verified"`
	}
}

func (s *Server) registerAuditRoutes() error {
	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/audit-events", authz.AuditRead, "read the audit log"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-audit-events", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/audit-events",
		Summary: "List audit events", Tags: []string{"audit"},
	}, func(ctx context.Context, in *auditInput) (*auditOutput, error) {
		actor, _ := actorFrom(ctx)
		events, verified, err := s.svc.ListAuditEvents(ctx, actor, in.Org, in.AfterSeq, in.Limit)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &auditOutput{}
		out.Body.Verified = verified
		labels := s.svc.AuditLabels(ctx, actor, in.Org, events)
		for _, event := range events {
			label := labels[event.ID]
			out.Body.Events = append(out.Body.Events, auditEventBody{
				ID:     event.ID,
				Seq:    event.Seq,
				Actor:  auditActor{ID: event.Actor.ID, Kind: event.Actor.Kind, Name: event.Actor.Name},
				Action: event.Action,
				Resource: auditRes{Type: event.Resource.Type, ID: event.Resource.ID, Name: event.Resource.Name, ProjectID: event.Resource.ProjectID, EnvID: event.Resource.EnvID,
					Project: label.Project, Environment: label.Environment, Key: label.Key},
				Details:   auditDetailFrom(event.Details),
				IP:        event.IP,
				UserAgent: event.UserAgent,
				CreatedAt: event.CreatedAt,
			})
		}
		return out, nil
	})
	if err := s.registry.Declare(http.MethodPost, "/api/v1/orgs/{org}/audit-events/checkpoint", authz.SettingsManage, "sign a new audit checkpoint"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-audit-checkpoint", Method: http.MethodPost, Path: "/api/v1/orgs/{org}/audit-events/checkpoint",
		Summary: "Sign an audit checkpoint", Tags: []string{"audit"}, DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *orgPath) (*checkpointOutput, error) {
		actor, _ := actorFrom(ctx)
		checkpoint, err := s.svc.CreateAuditCheckpoint(ctx, actor, in.Org, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		return &checkpointOutput{Body: checkpointBodyFrom(checkpoint)}, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/audit-events/checkpoints", authz.AuditRead, "list signed audit checkpoints"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "list-audit-checkpoints", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/audit-events/checkpoints",
		Summary: "List audit checkpoints", Tags: []string{"audit"},
	}, func(ctx context.Context, in *orgPath) (*checkpointsOutput, error) {
		actor, _ := actorFrom(ctx)
		checkpoints, err := s.svc.ListAuditCheckpoints(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &checkpointsOutput{}
		for i := range checkpoints {
			out.Body.Checkpoints = append(out.Body.Checkpoints, checkpointBodyFrom(&checkpoints[i]))
		}
		return out, nil
	})

	if err := s.registry.Declare(http.MethodGet, "/api/v1/orgs/{org}/audit-events/verify", authz.AuditRead, "verify the audit chain and checkpoints"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "verify-audit", Method: http.MethodGet, Path: "/api/v1/orgs/{org}/audit-events/verify",
		Summary: "Verify the audit chain", Tags: []string{"audit"},
	}, func(ctx context.Context, in *orgPath) (*auditVerifyOutput, error) {
		actor, _ := actorFrom(ctx)
		result, err := s.svc.VerifyAudit(ctx, actor, in.Org)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &auditVerifyOutput{}
		out.Body.ChainValid = result.ChainValid
		out.Body.Checkpoints = result.Checkpoints
		out.Body.LastSeq = result.LastSeq
		out.Body.PublicKey = result.PublicKey
		out.Body.Problem = result.Problem
		return out, nil
	})
	return nil
}

type checkpointBody struct {
	ID        string    `json:"id"`
	Seq       int64     `json:"seq"`
	EventHash string    `json:"event_hash"`
	PublicKey string    `json:"public_key"`
	CreatedAt time.Time `json:"created_at"`
}

type checkpointOutput struct {
	Body checkpointBody
}

type checkpointsOutput struct {
	Body struct {
		Checkpoints []checkpointBody `json:"checkpoints"`
	}
}

type auditVerifyOutput struct {
	Body struct {
		ChainValid  bool   `json:"chain_valid"`
		Checkpoints int    `json:"checkpoints"`
		LastSeq     int64  `json:"last_seq"`
		PublicKey   string `json:"public_key,omitempty"`
		Problem     string `json:"problem,omitempty"`
	}
}

func checkpointBodyFrom(checkpoint *audit.Checkpoint) checkpointBody {
	return checkpointBody{
		ID: checkpoint.ID, Seq: checkpoint.Seq, EventHash: checkpoint.EventHash,
		PublicKey: base64.StdEncoding.EncodeToString(checkpoint.PublicKey),
		CreatedAt: checkpoint.CreatedAt,
	}
}

func auditDetailFrom(d audit.Details) auditDetail {
	return auditDetail{
		KeyBlindIndex: d.KeyBlindIndex,
		Path:          d.Path,
		Environment:   d.Environment,
		Version:       d.Version,
		Reason:        d.Reason,
		Channel:       d.Channel,
		Command:       d.Command,
		Commands:      d.Commands,
		SchemaKey:     d.SchemaKey,
		RequestedKey:  d.RequestedKey,
		Count:         d.Count,
		RequestID:     d.RequestID,
	}
}
