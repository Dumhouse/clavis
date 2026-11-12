package server

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/dumhouse/clavis/internal/authz"
)

type createShareInput struct {
	Org     string `path:"org"`
	Project string `path:"project"`
	Env     string `path:"env"`
	Body    struct {
		Ciphertext string `json:"ciphertext" minLength:"1" maxLength:"1048576"`
		Nonce      string `json:"nonce" minLength:"1" maxLength:"64"`
		TTLHours   int    `json:"ttl_hours,omitempty" minimum:"1" maximum:"720" default:"24"`
		MaxViews   int    `json:"max_views,omitempty" minimum:"1" maximum:"100" default:"1"`
		Password   string `json:"password,omitempty" maxLength:"256"`
	}
}

type shareOutput struct {
	Body struct {
		ID        string `json:"id"`
		ExpiresAt string `json:"expires_at"`
		MaxViews  int    `json:"max_views"`
	}
}

type claimShareInput struct {
	ID   string `path:"id"`
	Body struct {
		Password string `json:"password,omitempty" maxLength:"256"`
	}
}

type claimShareOutput struct {
	Body struct {
		Ciphertext string `json:"ciphertext"`
		Nonce      string `json:"nonce"`
	}
}

func timeHours(hours int) time.Duration {
	if hours <= 0 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func (s *Server) registerShareRoutes() error {
	path := "/api/v1/orgs/{org}/projects/{project}/environments/{env}/shares"
	if err := s.registry.Declare(http.MethodPost, path, authz.SecretsReadValues, "create an encrypted one-time share link"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "create-share", Method: http.MethodPost, Path: path,
		Summary: "Create an encrypted one-time share link", Tags: []string{"shares"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *createShareInput) (*shareOutput, error) {
		actor, _ := actorFrom(ctx)
		view, err := s.svc.CreateShare(ctx, actor, in.Org, in.Project, in.Env,
			in.Body.Ciphertext, in.Body.Nonce,
			timeHours(in.Body.TTLHours), in.Body.MaxViews, in.Body.Password, metaFrom(ctx))
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &shareOutput{}
		out.Body.ID = view.ID
		out.Body.ExpiresAt = view.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.Body.MaxViews = view.MaxViews
		return out, nil
	})

	claimPath := "/api/v1/shares/{id}/claim"
	if err := s.registry.DeclarePublic(http.MethodPost, claimPath, "visitors claim encrypted share links without an account"); err != nil {
		return err
	}
	huma.Register(s.api, huma.Operation{
		OperationID: "claim-share", Method: http.MethodPost, Path: claimPath,
		Summary: "Claim an encrypted share link", Tags: []string{"shares"},
	}, func(ctx context.Context, in *claimShareInput) (*claimShareOutput, error) {
		claim, err := s.svc.ClaimShare(ctx, in.ID, in.Body.Password)
		if err != nil {
			return nil, s.apiError(ctx, err)
		}
		out := &claimShareOutput{}
		out.Body.Ciphertext = claim.Ciphertext
		out.Body.Nonce = claim.Nonce
		return out, nil
	})
	return nil
}
