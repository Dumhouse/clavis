package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// CISecretsInput stores Clavis CI tokens as secrets of one repository, so the
// "fetch when the job runs" setup needs no copy and paste.
type CISecretsInput struct {
	Provider     string
	BaseURL      string
	Token        string
	ConnectionID string
	Repo         string
	Secrets      map[string]string
}

// Only Clavis settings may be written this way; anything else is a sync.
var ciSecretName = regexp.MustCompile(`^CLAVIS_[A-Z0-9_]{1,60}$`)

// StoreCISecrets writes CLAVIS_* secrets into a Forgejo/Gitea, GitHub or
// GitLab repository with a platform token or a typed token. The values are
// passed through and not stored by Clavis.
func (s *Service) StoreCISecrets(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, in CISecretsInput, meta AuditMeta) ([]string, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	repo := strings.Trim(strings.TrimSpace(in.Repo), "/")
	if parts := strings.Split(repo, "/"); in.Provider != "gitlab" && (len(parts) != 2 || parts[0] == "" || parts[1] == "") {
		return nil, fmt.Errorf("%w: repository must be owner/name", ErrValidation)
	}
	if len(in.Secrets) == 0 {
		return nil, fmt.Errorf("%w: nothing to store", ErrValidation)
	}
	names := make([]string, 0, len(in.Secrets))
	for name, value := range in.Secrets {
		if !ciSecretName.MatchString(name) || value == "" {
			return nil, fmt.Errorf("%w: %q: only non-empty CLAVIS_* secrets can be stored here", ErrValidation, name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	// Reuse the sync push code with a throwaway sync that is never saved.
	sync := &store.Sync{ID: uuid.NewString(), ProjectID: project.ID, Provider: in.Provider}
	var config syncConfig
	switch {
	case in.ConnectionID != "":
		conn, err := s.Store.GetSyncConnection(ctx, org.ID, in.ConnectionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("%w: platform token not found", ErrValidation)
			}
			return nil, err
		}
		if conn.Provider != in.Provider {
			return nil, fmt.Errorf("%w: that credential is for %s", ErrValidation, conn.Provider)
		}
		sync.ConnectionID = conn.ID
		if err := s.applySyncConnection(ctx, org, sync, &config); err != nil {
			return nil, err
		}
	case strings.TrimSpace(in.Token) != "":
		if config.BaseURL, err = normalizeConnectionBaseURL(in.Provider, in.BaseURL); err != nil {
			return nil, err
		}
		if config.Token, err = s.encryptOrgSecret(org, sync.ID, "sync_token", strings.TrimSpace(in.Token)); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("%w: pick a platform token or enter an access token", ErrValidation)
	}

	kinds := map[string]string{}
	switch in.Provider {
	case "forgejo":
		config.ForgejoScope, config.ForgejoRepo = "repo", repo
		err = s.postForgejo(ctx, org, sync, config, in.Secrets, kinds)
	case "github":
		config.GitHubScope, config.GitHubRepo = "repo", repo
		if config.BaseURL == "" {
			config.BaseURL = "https://api.github.com"
		}
		err = s.postGitHub(ctx, org, sync, config, in.Secrets, kinds)
	case "gitlab":
		config.Project, config.Masked, config.Protected = repo, true, false
		err = s.postGitLab(ctx, org, sync, config, in.Secrets)
	default:
		return nil, fmt.Errorf("%w: storing CI secrets works for Forgejo, GitHub and GitLab", ErrValidation)
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrValidation, err)
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "ci_access.secrets_stored",
		audit.Resource{Type: "project", ID: project.ID, Name: project.Slug, ProjectID: project.ID},
		audit.Details{Reason: in.Provider + " " + repo + ": " + strings.Join(names, ", ")}, meta); err != nil {
		s.Logger.Error("audit ci_access.secrets_stored failed", "error", err)
	}
	return names, nil
}
