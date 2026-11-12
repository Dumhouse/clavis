package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

type githubPullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Merged bool `json:"merged"`
		Head   struct {
			Ref string `json:"ref"`
		} `json:"head"`
		Base struct {
			Ref string `json:"ref"`
		} `json:"base"`
	} `json:"pull_request"`
}

// HandleGitHubWebhook processes pull_request events into preview environments.
func (s *Service) HandleGitHubWebhook(ctx context.Context, orgSlug, projectSlug, event string, body []byte, signature string, meta AuditMeta) (string, error) {
	if event != "pull_request" {
		return "ignored", nil
	}
	org, err := s.Store.GetOrgBySlug(ctx, orgSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	project, err := s.Store.GetProjectBySlug(ctx, org.ID, projectSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	settings := ParseOrgSettings(org.Settings)
	if !settings.PreviewEnabled || len(settings.GitHubWebhookSecret) == 0 {
		return "", fmt.Errorf("%w: preview environments are not enabled", ErrForbidden)
	}
	secret, err := s.decryptOrgSecret(org, "github-preview", "webhook_secret", settings.GitHubWebhookSecret)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(signature)) {
		return "", fmt.Errorf("%w: invalid GitHub webhook signature", ErrForbidden)
	}
	var payload githubPullRequestEvent
	if err := json.Unmarshal(body, &payload); err != nil || payload.Number == 0 {
		return "", fmt.Errorf("%w: invalid pull request payload", ErrValidation)
	}
	if err := s.expirePreviews(ctx, org, project, meta); err != nil {
		s.Logger.Warn("preview cleanup failed", "project_id", project.ID, "error", err)
	}
	slug := fmt.Sprintf("pr-%d", payload.Number)
	switch payload.Action {
	case "opened", "reopened", "synchronize":
		existing, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, slug)
		if err == nil {
			if existing.PreviewSource == "github" {
				return "exists", nil
			}
			return "", fmt.Errorf("%w: environment %s already exists", ErrConflict, slug)
		}
		if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
		envs, err := s.Store.ListEnvironments(ctx, project.ID)
		if err != nil {
			return "", err
		}
		var parent *store.Environment
		position := len(envs)
		for i := range envs {
			if envs[i].Type == "development" && parent == nil {
				parent = &envs[i]
			}
			if envs[i].Position >= position {
				position = envs[i].Position + 1
			}
		}
		days := settings.PreviewRetentionDays
		if days <= 0 {
			days = 7
		}
		expires := s.Now().UTC().AddDate(0, 0, days)
		env := &store.Environment{
			ID: uuid.NewString(), ProjectID: project.ID, Slug: slug,
			Name: "PR #" + fmt.Sprintf("%d", payload.Number), Type: "preview",
			Color: "#8b5cf6", Position: position, BranchPattern: payload.PullRequest.Head.Ref,
			PreviewSource: "github", ExpiresAt: &expires,
		}
		if parent != nil {
			env.ParentID = &parent.ID
		}
		if err := s.Store.CreateEnvironment(ctx, env); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return "exists", nil
			}
			return "", err
		}
		if err := s.record(ctx, toAuditActor(authzActorWebhook()), org.ID, "env.preview.create",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug, Reason: payload.PullRequest.Head.Ref}, meta); err != nil {
			s.Logger.Error("audit env.preview.create failed", "env_id", env.ID, "error", err)
		}
		return "created", nil
	case "closed":
		env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, slug)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return "missing", nil
			}
			return "", err
		}
		if env.Type != "preview" || env.PreviewSource != "github" {
			return "", fmt.Errorf("%w: environment %s is not a managed preview", ErrConflict, slug)
		}
		if err := s.Store.DeleteEnvironment(ctx, env.ID); err != nil {
			return "", err
		}
		if err := s.record(ctx, toAuditActor(authzActorWebhook()), org.ID, "env.preview.delete",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug}, meta); err != nil {
			s.Logger.Error("audit env.preview.delete failed", "env_id", env.ID, "error", err)
		}
		return "deleted", nil
	default:
		return "ignored", nil
	}
}

func (s *Service) expirePreviews(ctx context.Context, org *store.Org, project *store.Project, meta AuditMeta) error {
	envs, err := s.Store.ListEnvironments(ctx, project.ID)
	if err != nil {
		return err
	}
	now := s.Now().UTC()
	for i := range envs {
		env := &envs[i]
		if env.Type != "preview" || (env.PreviewSource != "github" && env.PreviewSource != "branch") || env.ExpiresAt == nil {
			continue
		}
		if now.Before(*env.ExpiresAt) {
			continue
		}
		if err := s.Store.DeleteEnvironment(ctx, env.ID); err != nil {
			continue
		}
		_ = s.record(ctx, toAuditActor(authzActorWebhook()), org.ID, "env.preview.expire",
			audit.Resource{Type: "environment", ID: env.ID, Name: env.Slug, ProjectID: project.ID, EnvID: env.ID},
			audit.Details{Environment: env.Slug}, meta)
	}
	return nil
}

func authzActorWebhook() authz.Actor {
	return authz.Actor{ID: "github-webhook", Kind: authz.SubjectMachine, Name: "github-webhook"}
}
