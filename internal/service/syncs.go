package service

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/nacl/box"

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ErrSyncUnconfirmed requires an explicit confirmation before the first push.
var ErrSyncUnconfirmed = errors.New("service: sync requires first-run confirmation")

// ErrEnvironmentFrozen blocks changes during a freeze window.
var ErrEnvironmentFrozen = errors.New("service: environment is frozen")

type syncConfig struct {
	Endpoint          string `json:"endpoint,omitempty"`
	Secret            []byte `json:"secret,omitempty"`
	BaseURL           string `json:"base_url,omitempty"`
	Project           string `json:"project,omitempty"`
	Token             []byte `json:"token,omitempty"`
	Protected         bool   `json:"protected,omitempty"`
	Masked            bool   `json:"masked,omitempty"`
	GitHubRepo        string `json:"github_repo,omitempty"`
	GitHubEnv         string `json:"github_environment,omitempty"`
	GitHubScope       string `json:"github_scope,omitempty"`
	DeleteMissing     bool   `json:"delete_missing,omitempty"`
	VercelProject     string `json:"vercel_project,omitempty"`
	VercelTeamID      string `json:"vercel_team_id,omitempty"`
	AWSRegion         string `json:"aws_region,omitempty"`
	AWSAccessKey      string `json:"aws_access_key,omitempty"`
	AWSSecret         []byte `json:"aws_secret,omitempty"`
	AWSService        string `json:"aws_service,omitempty"`
	AWSPrefix         string `json:"aws_prefix,omitempty"`
	AWSSessionToken   string `json:"aws_session_token,omitempty"`
	GCPProject        string `json:"gcp_project,omitempty"`
	GCPServiceAccount []byte `json:"gcp_service_account,omitempty"`
	GCPPrefix         string `json:"gcp_prefix,omitempty"`
	NetlifyAccount    string `json:"netlify_account,omitempty"`
	NetlifySite       string `json:"netlify_site,omitempty"`
	CFAccount         string `json:"cf_account,omitempty"`
	CFProject         string `json:"cf_project,omitempty"`
	CFScript          string `json:"cf_script,omitempty"`
	CFMode            string `json:"cf_mode,omitempty"`
}

// SyncInput creates a sync definition.
type SyncInput struct {
	EnvironmentSlug   string
	Provider          string
	Endpoint          string
	Secret            string
	BaseURL           string
	GitLabProject     string
	Token             string
	Protected         bool
	Masked            bool
	GitHubRepo        string
	GitHubEnvironment string
	GitHubScope       string
	VercelProject     string
	VercelTeamID      string
	AWSRegion         string
	AWSAccessKey      string
	AWSSecretKey      string
	AWSSessionToken   string
	AWSService        string
	AWSPrefix         string
	GCPProject        string
	GCPServiceAccount string
	GCPPrefix         string
	NetlifyAccount    string
	NetlifySite       string
	CloudflareAccount string
	CloudflareProject string
	CloudflareScript  string
	CloudflareMode    string
	DeleteMissing     bool
}

// SyncView is a sync plus its environment slug.
type SyncView struct {
	Sync        *store.Sync
	Environment string
}

// SyncPreview describes what a run would send.
type SyncPreview struct {
	SyncID      string
	Destination string
	Environment string
	Keys        []string
	Count       int
	Confirmed   bool
}

// SyncRunResult is the outcome of a run.
type SyncRunResult struct {
	Run     *store.SyncRun
	Preview *SyncPreview
}

// CreateSync stores a generic HTTP sync.
func (s *Service) CreateSync(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, in SyncInput, meta AuditMeta) (*SyncView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	env, err := s.Store.GetEnvironmentBySlug(ctx, project.ID, in.EnvironmentSlug)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(in.Provider))
	if provider == "" {
		provider = "http"
	}
	if provider != "http" && provider != "gitlab" && provider != "github" && provider != "vercel" && provider != "aws" && provider != "gcp" && provider != "netlify" && provider != "cloudflare" {
		return nil, fmt.Errorf("%w: unsupported sync provider %q", ErrValidation, provider)
	}
	id := uuid.NewString()
	config := syncConfig{Protected: in.Protected, Masked: in.Masked, DeleteMissing: in.DeleteMissing}
	endpoint := strings.TrimSpace(in.Endpoint)
	if provider == "http" {
		if !strings.HasPrefix(endpoint, "https://") && !strings.HasPrefix(endpoint, "http://") {
			return nil, fmt.Errorf("%w: sync endpoint must be http(s)", ErrValidation)
		}
		config.Endpoint = endpoint
		if in.Secret != "" {
			ciphertext, err := s.encryptOrgSecret(org, id, "sync_secret", in.Secret)
			if err != nil {
				return nil, err
			}
			config.Secret = ciphertext
		}
	} else if provider == "netlify" {
		config.NetlifyAccount = strings.TrimSpace(in.NetlifyAccount)
		config.NetlifySite = strings.TrimSpace(in.NetlifySite)
		if config.NetlifyAccount == "" || in.Token == "" {
			return nil, fmt.Errorf("%w: Netlify account slug and token are required", ErrValidation)
		}
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if config.BaseURL == "" {
			config.BaseURL = "https://api.netlify.com"
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "sync_token", in.Token)
		if err != nil {
			return nil, err
		}
		config.Token = ciphertext
	} else if provider == "cloudflare" {
		config.CFAccount = strings.TrimSpace(in.CloudflareAccount)
		config.CFProject = strings.TrimSpace(in.CloudflareProject)
		config.CFScript = strings.TrimSpace(in.CloudflareScript)
		config.CFMode = strings.ToLower(strings.TrimSpace(in.CloudflareMode))
		if config.CFMode == "" {
			config.CFMode = "workers"
		}
		if config.CFAccount == "" || in.Token == "" {
			return nil, fmt.Errorf("%w: Cloudflare account ID and token are required", ErrValidation)
		}
		if config.CFMode == "workers" && config.CFScript == "" {
			return nil, fmt.Errorf("%w: Cloudflare Workers mode needs a script name", ErrValidation)
		}
		if config.CFMode == "pages" && config.CFProject == "" {
			return nil, fmt.Errorf("%w: Cloudflare Pages mode needs a project name", ErrValidation)
		}
		if config.CFMode != "workers" && config.CFMode != "pages" {
			return nil, fmt.Errorf("%w: Cloudflare mode must be workers or pages", ErrValidation)
		}
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if config.BaseURL == "" {
			config.BaseURL = "https://api.cloudflare.com"
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "sync_token", in.Token)
		if err != nil {
			return nil, err
		}
		config.Token = ciphertext
	} else if provider == "gcp" {
		config.GCPProject = strings.TrimSpace(in.GCPProject)
		config.GCPPrefix = strings.Trim(strings.TrimSpace(in.GCPPrefix), "-")
		if config.GCPProject == "" || strings.TrimSpace(in.GCPServiceAccount) == "" {
			return nil, fmt.Errorf("%w: GCP project and service account JSON are required", ErrValidation)
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "gcp_service_account", in.GCPServiceAccount)
		if err != nil {
			return nil, err
		}
		config.GCPServiceAccount = ciphertext
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	} else if provider == "aws" {
		config.AWSRegion = strings.TrimSpace(in.AWSRegion)
		config.AWSService = strings.ToLower(strings.TrimSpace(in.AWSService))
		config.AWSPrefix = strings.Trim(strings.TrimSpace(in.AWSPrefix), "/")
		config.AWSAccessKey = strings.TrimSpace(in.AWSAccessKey)
		if config.AWSRegion == "" || config.AWSAccessKey == "" || in.AWSSecretKey == "" {
			return nil, fmt.Errorf("%w: AWS region, access key, and secret key are required", ErrValidation)
		}
		if config.AWSService == "" {
			config.AWSService = "ssm"
		}
		if config.AWSService != "ssm" && config.AWSService != "secretsmanager" {
			return nil, fmt.Errorf("%w: AWS service must be ssm or secretsmanager", ErrValidation)
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "aws_secret", in.AWSSecretKey)
		if err != nil {
			return nil, err
		}
		config.AWSSecret = ciphertext
		config.AWSSessionToken = strings.TrimSpace(in.AWSSessionToken)
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	} else if provider == "vercel" {
		config.VercelProject = strings.TrimSpace(in.VercelProject)
		if config.VercelProject == "" {
			return nil, fmt.Errorf("%w: Vercel project ID or name is required", ErrValidation)
		}
		config.VercelTeamID = strings.TrimSpace(in.VercelTeamID)
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if config.BaseURL == "" {
			config.BaseURL = "https://api.vercel.com"
		}
		if in.Token == "" {
			return nil, fmt.Errorf("%w: Vercel access token is required", ErrValidation)
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "sync_token", in.Token)
		if err != nil {
			return nil, err
		}
		config.Token = ciphertext
	} else if provider == "github" {
		config.GitHubRepo = strings.TrimSpace(in.GitHubRepo)
		config.GitHubEnv = strings.TrimSpace(in.GitHubEnvironment)
		config.GitHubScope = strings.ToLower(strings.TrimSpace(in.GitHubScope))
		if config.GitHubScope == "" {
			config.GitHubScope = "repo"
		}
		switch config.GitHubScope {
		case "repo":
			if !strings.Contains(config.GitHubRepo, "/") {
				return nil, fmt.Errorf("%w: GitHub repository must be owner/name", ErrValidation)
			}
		case "environment":
			if !strings.Contains(config.GitHubRepo, "/") || config.GitHubEnv == "" {
				return nil, fmt.Errorf("%w: GitHub environment sync needs repository and environment", ErrValidation)
			}
		case "org":
			if config.GitHubRepo == "" {
				return nil, fmt.Errorf("%w: GitHub org sync needs an organization", ErrValidation)
			}
		default:
			return nil, fmt.Errorf("%w: GitHub scope must be repo, environment, or org", ErrValidation)
		}
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if config.BaseURL == "" {
			config.BaseURL = "https://api.github.com"
		}
		if in.Token == "" {
			return nil, fmt.Errorf("%w: GitHub token is required", ErrValidation)
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "sync_token", in.Token)
		if err != nil {
			return nil, err
		}
		config.Token = ciphertext
	} else {
		config.Project = strings.TrimSpace(in.GitLabProject)
		if config.Project == "" {
			return nil, fmt.Errorf("%w: GitLab project path or ID is required", ErrValidation)
		}
		config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
		if config.BaseURL == "" {
			config.BaseURL = "https://gitlab.com"
		}
		if !strings.HasPrefix(config.BaseURL, "https://") && !strings.HasPrefix(config.BaseURL, "http://") {
			return nil, fmt.Errorf("%w: GitLab URL must be http(s)", ErrValidation)
		}
		if in.Token == "" {
			return nil, fmt.Errorf("%w: GitLab access token is required", ErrValidation)
		}
		ciphertext, err := s.encryptOrgSecret(org, id, "sync_token", in.Token)
		if err != nil {
			return nil, err
		}
		config.Token = ciphertext
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	sync := &store.Sync{
		ID: id, ProjectID: project.ID, EnvironmentID: env.ID, Path: "/",
		Provider: provider, Config: string(encoded),
	}
	if err := s.Store.CreateSync(ctx, sync); err != nil {
		return nil, err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "sync.create",
		audit.Resource{Type: "sync", ID: sync.ID, Name: provider, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug}, meta); err != nil {
		s.Logger.Error("audit sync.create failed", "sync_id", sync.ID, "error", err)
	}
	return &SyncView{Sync: sync, Environment: env.Slug}, nil
}

// ListSyncs returns syncs for a project.
func (s *Service) ListSyncs(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string) ([]SyncView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	_ = org
	syncs, err := s.Store.ListSyncs(ctx, project.ID)
	if err != nil {
		return nil, err
	}
	out := make([]SyncView, 0, len(syncs))
	for i := range syncs {
		env, err := s.Store.GetEnvironmentByID(ctx, syncs[i].EnvironmentID)
		name := ""
		if err == nil {
			name = env.Slug
		}
		out = append(out, SyncView{Sync: &syncs[i], Environment: name})
	}
	return out, nil
}

// DeleteSync removes a sync.
func (s *Service) DeleteSync(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, id string, meta AuditMeta) error {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteSync(ctx, project.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "sync.delete",
		audit.Resource{Type: "sync", ID: id, ProjectID: project.ID}, audit.Details{}, meta); err != nil {
		s.Logger.Error("audit sync.delete failed", "sync_id", id, "error", err)
	}
	return nil
}

// ListSyncRuns returns recent run history.
func (s *Service) ListSyncRuns(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, id string, limit int) ([]store.SyncRun, error) {
	_, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.GetSync(ctx, project.ID, id); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return s.Store.ListSyncRuns(ctx, id, limit)
}

// RunSync previews or executes a sync.
func (s *Service) RunSync(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, id string, dryRun, confirm bool, meta AuditMeta) (*SyncRunResult, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	sync, err := s.Store.GetSync(ctx, project.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	env, err := s.Store.GetEnvironmentByID(ctx, sync.EnvironmentID)
	if err != nil {
		return nil, err
	}
	values, kinds, err := s.syncValues(ctx, org, project, env)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var config syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
		return nil, err
	}
	destination := config.Endpoint
	if sync.Provider == "gitlab" {
		destination = config.BaseURL + "/" + config.Project
	}
	if sync.Provider == "github" {
		destination = config.BaseURL + " " + config.GitHubScope + ":" + config.GitHubRepo
	}
	if sync.Provider == "vercel" {
		destination = config.BaseURL + " project:" + config.VercelProject
	}
	if sync.Provider == "aws" {
		destination = fmt.Sprintf("aws:%s %s/%s", config.AWSService, config.AWSRegion, config.AWSPrefix)
	}
	if sync.Provider == "gcp" {
		destination = "gcp:" + config.GCPProject
	}
	if sync.Provider == "netlify" {
		destination = "netlify:" + config.NetlifyAccount + "/" + config.NetlifySite
	}
	if sync.Provider == "cloudflare" {
		destination = "cloudflare:" + config.CFMode + " " + config.CFAccount
	}
	preview := &SyncPreview{
		SyncID: sync.ID, Destination: destination, Environment: env.Slug,
		Keys: keys, Count: len(keys), Confirmed: sync.Confirmed,
	}
	if dryRun {
		now := s.Now().UTC()
		run := &store.SyncRun{
			ID: uuid.NewString(), SyncID: sync.ID, ProjectID: project.ID, Status: "preview",
			DryRun: true, SecretCount: len(keys), StartedAt: now, FinishedAt: &now,
			Detail: "preview only; no values sent",
		}
		if err := s.Store.CreateSyncRun(ctx, run); err != nil {
			return nil, err
		}
		return &SyncRunResult{Run: run, Preview: preview}, nil
	}
	if !sync.Confirmed && !confirm {
		return &SyncRunResult{Preview: preview}, ErrSyncUnconfirmed
	}

	if freeze, freezeErr := s.Store.ActiveFreeze(ctx, env.ID, s.Now().UTC()); freezeErr == nil && freeze != nil {
		return nil, fmt.Errorf("%w: %s until %s", ErrEnvironmentFrozen, freeze.Reason, freeze.EndsAt.Format(time.RFC3339))
	}
	now := s.Now().UTC()
	run := &store.SyncRun{
		ID: uuid.NewString(), SyncID: sync.ID, ProjectID: project.ID, Status: "running",
		SecretCount: len(keys), StartedAt: now,
	}
	if err := s.Store.CreateSyncRun(ctx, run); err != nil {
		return nil, err
	}
	var err2 error
	if sync.Provider == "gitlab" {
		err2 = s.postGitLab(ctx, org, sync, config, values)
	} else if sync.Provider == "github" {
		err2 = s.postGitHub(ctx, org, sync, config, values, kinds)
	} else if sync.Provider == "vercel" {
		err2 = s.postVercel(ctx, org, sync, config, env, values, kinds)
	} else if sync.Provider == "aws" {
		err2 = s.postAWS(ctx, org, sync, config, project, env, values, kinds)
	} else if sync.Provider == "gcp" {
		err2 = s.postGCP(ctx, org, sync, config, project, env, values)
	} else if sync.Provider == "netlify" {
		err2 = s.postNetlify(ctx, org, sync, config, env, values, kinds)
	} else if sync.Provider == "cloudflare" {
		err2 = s.postCloudflare(ctx, org, sync, config, env, values, kinds)
	} else {
		secret, err := s.decryptOrgSecret(org, sync.ID, "sync_secret", config.Secret)
		if err != nil {
			return nil, err
		}
		payload, err := json.Marshal(map[string]any{
			"project": project.Slug, "environment": env.Slug, "secrets": values,
		})
		if err == nil {
			err2 = s.postSync(ctx, run, sync, payload, secret)
		} else {
			err2 = err
		}
	}
	err = err2
	if err != nil {
		finished := s.Now().UTC()
		run.Status = "failed"
		run.Error = err.Error()
		run.FinishedAt = &finished
		_ = s.Store.UpdateSyncRun(ctx, run)
		_ = s.Store.UpdateSyncStatus(ctx, sync.ID, "failed", nil, sync.Confirmed)
		return &SyncRunResult{Run: run, Preview: preview}, nil
	}
	finished := s.Now().UTC()
	run.Status = "success"
	run.FinishedAt = &finished
	_ = s.Store.UpdateSyncRun(ctx, run)
	_ = s.Store.UpdateSyncStatus(ctx, sync.ID, "success", &finished, true)
	if err := s.record(ctx, toAuditActor(actor), org.ID, "sync.run",
		audit.Resource{Type: "sync", ID: sync.ID, Name: sync.Provider, ProjectID: project.ID, EnvID: env.ID},
		audit.Details{Environment: env.Slug, Count: len(keys)}, meta); err != nil {
		s.Logger.Error("audit sync.run failed", "sync_id", sync.ID, "error", err)
	}
	return &SyncRunResult{Run: run, Preview: preview}, nil
}

func (s *Service) postSync(ctx context.Context, run *store.SyncRun, sync *store.Sync, payload []byte, secret string) error {
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	var config syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
		return err
	}
	backoff := time.Second
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, config.Endpoint, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("sync: invalid destination: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("User-Agent", "clavis-sync/1")
		request.Header.Set("X-Clavis-Sync", sync.ID)
		request.Header.Set("Idempotency-Key", run.ID)
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(payload)
			request.Header.Set("X-Clavis-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		response, err := client.Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			status := response.StatusCode
			_ = response.Body.Close()
			if status >= 200 && status < 300 {
				return nil
			}
			lastErr = fmt.Errorf("destination returned %d; check the endpoint URL and receiver logs", status)
		} else {
			lastErr = fmt.Errorf("%v; check the endpoint URL, DNS, and network policy", err)
		}
		if attempt < 3 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
	}
	return lastErr
}

func (s *Service) syncValues(ctx context.Context, org *store.Org, project *store.Project, env *store.Environment) (map[string]string, map[string]string, error) {
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	kinds := map[string]string{}
	chain, err := s.environmentChain(ctx, env)
	if err != nil {
		return nil, nil, err
	}
	seen := make(map[string]bool)
	var secrets []Secret
	for _, sourceEnv := range chain {
		items, err := s.Store.ListItems(ctx, sourceEnv.ID)
		if err != nil {
			return nil, nil, err
		}
		for i := range items {
			item := &items[i]
			key, err := s.decryptKey(valueKey, org, project, sourceEnv, item)
			if err != nil {
				return nil, nil, err
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			value := ""
			if len(item.ValueCiphertext) > 0 {
				decrypted, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
				if err != nil {
					return nil, nil, err
				}
				value = string(decrypted)
			}
			kinds[key] = item.Kind
			secrets = append(secrets, Secret{Key: key, Kind: item.Kind, Value: value})
		}
	}
	sharedSets, err := s.sharedSetValues(ctx, org, env)
	if err != nil {
		return nil, nil, err
	}
	applySharedSetReferences(secrets, sharedSets)
	for _, key := range sortedSharedKeys(sharedSets) {
		if seen[key] {
			continue
		}
		value := sharedSets[key]
		kinds[key] = value.Kind
		secrets = append(secrets, Secret{Key: key, Kind: value.Kind, Value: value.Value})
	}
	if err := resolveSecretReferences(secrets); err != nil {
		return nil, nil, err
	}
	values := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		values[secret.Key] = secret.Value
	}
	return values, kinds, nil
}

func (s *Service) postGitLab(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values map[string]string) error {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	projectPath := url.PathEscape(config.Project)
	variablesURL := base + "/api/v4/projects/" + projectPath + "/variables"

	existing := map[string]bool{}
	listReq, err := http.NewRequestWithContext(ctx, http.MethodGet, variablesURL+"?per_page=100", nil)
	if err != nil {
		return err
	}
	listReq.Header.Set("PRIVATE-TOKEN", token)
	listResp, err := client.Do(listReq)
	if err != nil {
		return fmt.Errorf("gitlab: could not list variables: %v; check the GitLab URL and network policy", err)
	}
	listBody, _ := io.ReadAll(io.LimitReader(listResp.Body, 1<<20))
	_ = listResp.Body.Close()
	switch listResp.StatusCode {
	case http.StatusOK:
		var variables []struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(listBody, &variables); err == nil {
			for _, variable := range variables {
				existing[variable.Key] = true
			}
		}
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("gitlab: access token rejected; check the token scope (api) and expiry")
	case http.StatusNotFound:
		return fmt.Errorf("gitlab: project %q not found; check the project path and token access", config.Project)
	default:
		return fmt.Errorf("gitlab: variable list returned %d; check the project and token", listResp.StatusCode)
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := values[key]
		masked := config.Masked && len(value) >= 8 && !strings.ContainsAny(value, " \n\t\r")
		form := url.Values{}
		form.Set("value", value)
		form.Set("protected", fmt.Sprintf("%t", config.Protected))
		form.Set("masked", fmt.Sprintf("%t", masked))
		if key == "" {
			continue
		}
		method := http.MethodPut
		endpoint := variablesURL + "/" + url.PathEscape(key)
		if !existing[key] {
			method = http.MethodPost
			endpoint = variablesURL
			form.Set("key", key)
		}
		request, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(form.Encode()))
		if err != nil {
			return err
		}
		request.Header.Set("PRIVATE-TOKEN", token)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("gitlab: request failed: %v; check the GitLab URL and network policy", err)
		}
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8192))
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			detail := strings.TrimSpace(string(body))
			if len(detail) > 300 {
				detail = detail[:300]
			}
			return fmt.Errorf("gitlab: setting %s returned %d: %s", key, response.StatusCode, detail)
		}
	}
	if config.DeleteMissing {
		for remoteKey := range existing {
			if _, ok := values[remoteKey]; ok {
				continue
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodDelete,
				variablesURL+"/"+url.PathEscape(remoteKey), nil)
			if err != nil {
				return err
			}
			request.Header.Set("PRIVATE-TOKEN", token)
			response, err := client.Do(request)
			if err != nil {
				return fmt.Errorf("gitlab: delete %s failed: %v", remoteKey, err)
			}
			_ = response.Body.Close()
			if response.StatusCode >= 300 && response.StatusCode != http.StatusNotFound {
				return fmt.Errorf("gitlab: delete %s returned %d", remoteKey, response.StatusCode)
			}
		}
	}
	return nil
}

func (s *Service) postGitHub(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values, kinds map[string]string) error {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	var actions string
	switch config.GitHubScope {
	case "org":
		actions = base + "/orgs/" + url.PathEscape(config.GitHubRepo) + "/actions"
	case "environment":
		parts := strings.SplitN(config.GitHubRepo, "/", 2)
		actions = base + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) +
			"/environments/" + url.PathEscape(config.GitHubEnv) + "/actions"
	default:
		parts := strings.SplitN(config.GitHubRepo, "/", 2)
		actions = base + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/actions"
	}

	var secrets []string
	for key := range values {
		if kinds[key] != "variable" {
			secrets = append(secrets, key)
		}
	}
	if len(secrets) > 0 {
		var keyInfo struct {
			KeyID string `json:"key_id"`
			Key   string `json:"key"`
		}
		status, body, err := githubRequest(ctx, client, http.MethodGet, actions+"/secrets/public-key", token, nil)
		if err != nil {
			return fmt.Errorf("github: public key request failed: %v; check the API URL and network policy", err)
		}
		if status < 200 || status >= 300 {
			return githubStatusError("public key", status, body)
		}
		if err := json.Unmarshal(body, &keyInfo); err != nil {
			return fmt.Errorf("github: invalid public key response")
		}
		rawKey, err := base64.StdEncoding.DecodeString(keyInfo.Key)
		if err != nil || len(rawKey) != 32 {
			return fmt.Errorf("github: invalid secret public key")
		}
		var publicKey [32]byte
		copy(publicKey[:], rawKey)
		sort.Strings(secrets)
		for _, name := range secrets {
			if !validGitHubName(name) {
				return fmt.Errorf("github: %q is not a valid Actions secret name (letters, digits, underscore; cannot start with GITHUB_)", name)
			}
			sealed, err := box.SealAnonymous(nil, []byte(values[name]), &publicKey, rand.Reader)
			if err != nil {
				return fmt.Errorf("github: encrypt secret %s: %w", name, err)
			}
			payload, _ := json.Marshal(map[string]string{
				"encrypted_value": base64.StdEncoding.EncodeToString(sealed),
				"key_id":          keyInfo.KeyID,
			})
			status, body, err := githubRequest(ctx, client, http.MethodPut, actions+"/secrets/"+url.PathEscape(name), token, payload)
			if err != nil {
				return fmt.Errorf("github: secret update failed: %v; check the API URL and network policy", err)
			}
			if status < 200 || status >= 300 {
				return githubStatusError("secret "+name, status, body)
			}
		}
	}

	var variableNames []string
	for key := range values {
		if kinds[key] == "variable" {
			variableNames = append(variableNames, key)
		}
	}
	if len(variableNames) == 0 {
		return nil
	}
	existing := map[string]bool{}
	status, body, err := githubRequest(ctx, client, http.MethodGet, actions+"/variables?per_page=100", token, nil)
	if err != nil {
		return fmt.Errorf("github: variable list failed: %v; check the API URL and network policy", err)
	}
	if status == http.StatusOK {
		var listing struct {
			Variables []struct {
				Name string `json:"name"`
			} `json:"variables"`
		}
		if json.Unmarshal(body, &listing) == nil {
			for _, variable := range listing.Variables {
				existing[variable.Name] = true
			}
		}
	} else if status != http.StatusNotFound {
		return githubStatusError("variable list", status, body)
	}
	sort.Strings(variableNames)
	for _, name := range variableNames {
		if !validGitHubName(name) {
			return fmt.Errorf("github: %q is not a valid Actions variable name (letters, digits, underscore; cannot start with GITHUB_)", name)
		}
		payload, _ := json.Marshal(map[string]string{"name": name, "value": values[name]})
		method := http.MethodPost
		endpoint := actions + "/variables"
		if existing[name] {
			method = http.MethodPatch
			endpoint = actions + "/variables/" + url.PathEscape(name)
		}
		status, body, err := githubRequest(ctx, client, method, endpoint, token, payload)
		if err != nil {
			return fmt.Errorf("github: variable update failed: %v; check the API URL and network policy", err)
		}
		if status < 200 || status >= 300 {
			return githubStatusError("variable "+name, status, body)
		}
	}
	if config.DeleteMissing {
		for name := range existing {
			if _, keep := values[name]; keep {
				continue
			}
			status, body, err := githubRequest(ctx, client, http.MethodDelete,
				actions+"/variables/"+url.PathEscape(name), token, nil)
			if err != nil {
				return fmt.Errorf("github: delete variable %s failed: %v", name, err)
			}
			if status >= 300 && status != http.StatusNotFound {
				return githubStatusError("delete variable "+name, status, body)
			}
		}
		status, body, err := githubRequest(ctx, client, http.MethodGet, actions+"/secrets?per_page=100", token, nil)
		if err == nil && status == http.StatusOK {
			var listing struct {
				Secrets []struct {
					Name string `json:"name"`
				} `json:"secrets"`
			}
			if json.Unmarshal(body, &listing) == nil {
				for _, remote := range listing.Secrets {
					if _, keep := values[remote.Name]; keep {
						continue
					}
					status, body, err := githubRequest(ctx, client, http.MethodDelete,
						actions+"/secrets/"+url.PathEscape(remote.Name), token, nil)
					if err != nil {
						return fmt.Errorf("github: delete secret %s failed: %v", remote.Name, err)
					}
					if status >= 300 && status != http.StatusNotFound {
						return githubStatusError("delete secret "+remote.Name, status, body)
					}
				}
			}
		}
	}
	return nil
}

func githubRequest(ctx context.Context, client *http.Client, method, endpoint, token string, payload []byte) (int, []byte, error) {
	var reader *bytes.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "clavis-sync/1")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

func githubStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("github: token rejected; check that it has Actions secrets/variables write permission")
	case http.StatusForbidden:
		return fmt.Errorf("github: permission denied for %s; check token scopes and repository/org policy", operation)
	case http.StatusNotFound:
		return fmt.Errorf("github: %s not found; check the repository, environment, and API URL", operation)
	default:
		return fmt.Errorf("github: %s returned %d: %s", operation, status, detail)
	}
}

func validGitHubName(name string) bool {
	if name == "" || strings.HasPrefix(strings.ToUpper(name), "GITHUB_") {
		return false
	}
	for i, r := range name {
		if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
			continue
		}
		if i > 0 && r >= '0' && r <= '9' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) postVercel(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, env *store.Environment, values, kinds map[string]string) error {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	project := url.PathEscape(config.VercelProject)
	query := ""
	if config.VercelTeamID != "" {
		query = "?teamId=" + url.QueryEscape(config.VercelTeamID)
	}
	target := []string{"development"}
	switch env.Type {
	case "production":
		target = []string{"production"}
	case "preview", "staging":
		target = []string{"preview"}
	}
	existing := map[string]string{}
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, base+"/v10/projects/"+project+"/env"+query, token, nil)
	if err != nil {
		return fmt.Errorf("vercel: could not list variables: %v; check the API URL and network policy", err)
	}
	if status == http.StatusOK {
		var listing struct {
			Envs []struct {
				ID  string `json:"id"`
				Key string `json:"key"`
			} `json:"envs"`
		}
		if json.Unmarshal(body, &listing) == nil {
			for _, item := range listing.Envs {
				existing[item.Key] = item.ID
			}
		}
	} else if status != http.StatusNotFound {
		return vercelStatusError("list variables", status, body)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		variableType := "encrypted"
		if kinds[key] == "variable" {
			variableType = "plain"
		}
		payload, _ := json.Marshal(map[string]any{
			"key": key, "value": values[key], "type": variableType, "target": target,
		})
		method := http.MethodPost
		endpoint := base + "/v10/projects/" + project + "/env" + query
		if id := existing[key]; id != "" {
			method = http.MethodPatch
			endpoint = base + "/v10/projects/" + project + "/env/" + url.PathEscape(id) + query
		}
		status, body, err := githubLikeRequest(ctx, client, method, endpoint, token, payload)
		if err != nil {
			return fmt.Errorf("vercel: request failed: %v; check the API URL and network policy", err)
		}
		if status < 200 || status >= 300 {
			return vercelStatusError("set "+key, status, body)
		}
	}
	if config.DeleteMissing {
		for remoteKey, id := range existing {
			if _, keep := values[remoteKey]; keep {
				continue
			}
			status, body, err := githubLikeRequest(ctx, client, http.MethodDelete,
				base+"/v10/projects/"+project+"/env/"+url.PathEscape(id)+query, token, nil)
			if err != nil {
				return fmt.Errorf("vercel: delete %s failed: %v", remoteKey, err)
			}
			if status >= 300 && status != http.StatusNotFound {
				return vercelStatusError("delete "+remoteKey, status, body)
			}
		}
	}
	return nil
}

// githubLikeRequest performs a bearer-authenticated JSON request used by
// providers with GitHub-style APIs.
func githubLikeRequest(ctx context.Context, client *http.Client, method, endpoint, token string, payload []byte) (int, []byte, error) {
	var reader *bytes.Reader
	if payload != nil {
		reader = bytes.NewReader(payload)
	} else {
		reader = bytes.NewReader(nil)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "clavis-sync/1")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

func vercelStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("vercel: token rejected; check the access token")
	case http.StatusForbidden:
		return fmt.Errorf("vercel: permission denied for %s; check project and team access", operation)
	case http.StatusNotFound:
		return fmt.Errorf("vercel: %s not found; check the project ID/name and team ID", operation)
	default:
		return fmt.Errorf("vercel: %s returned %d: %s", operation, status, detail)
	}
}

func (s *Service) postAWS(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, project *store.Project, env *store.Environment, values, kinds map[string]string) error {
	secretKey, err := s.decryptOrgSecret(org, sync.ID, "aws_secret", config.AWSSecret)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	endpoint := config.BaseURL
	if endpoint == "" {
		endpoint = "https://" + config.AWSService + "." + config.AWSRegion + ".amazonaws.com"
	}
	prefix := strings.Trim(config.AWSPrefix, "/")
	if prefix == "" {
		prefix = "clavis/" + project.Slug + "/" + env.Slug
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := prefix + "/" + key
		if config.AWSService == "ssm" {
			parameterType := "SecureString"
			if kinds[key] == "variable" {
				parameterType = "String"
			}
			body, _ := json.Marshal(map[string]any{
				"Name": name, "Value": values[key], "Type": parameterType, "Overwrite": true,
			})
			if err := s.awsJSONCall(ctx, client, endpoint, "AmazonSSM.PutParameter", "ssm",
				config, secretKey, body); err != nil {
				return err
			}
			continue
		}
		body, _ := json.Marshal(map[string]any{"Name": name, "SecretString": values[key]})
		err := s.awsJSONCall(ctx, client, endpoint, "secretsmanager.CreateSecret", "secretsmanager",
			config, secretKey, body)
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "ResourceExistsException") {
			return err
		}
		body, _ = json.Marshal(map[string]any{"SecretId": name, "SecretString": values[key]})
		if err := s.awsJSONCall(ctx, client, endpoint, "secretsmanager.PutSecretValue", "secretsmanager",
			config, secretKey, body); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) awsJSONCall(ctx context.Context, client *http.Client, endpoint, target, service string, config syncConfig, secretKey string, payload []byte) error {
	status, body, err := awsJSONResponse(ctx, client, endpoint, target, service, config, secretKey, payload)
	if err != nil {
		return err
	}
	if status >= 200 && status < 300 {
		return nil
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Errorf("aws: credentials rejected; check the access key, secret, and IAM permissions")
	case http.StatusBadRequest:
		return fmt.Errorf("aws: %s: %s", target, detail)
	default:
		return fmt.Errorf("aws: %s returned %d: %s", target, status, detail)
	}
}

func awsJSONResponse(ctx context.Context, client *http.Client, endpoint, target, service string, config syncConfig, secretKey string, payload []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", target)
	signAWSJSON(request, payload, service, config, secretKey)
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("aws: request failed: %v; check the region, endpoint, and network policy", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

func signAWSJSON(req *http.Request, payload []byte, service string, config syncConfig, secretKey string) {
	now := time.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadHash := sha256.Sum256(payload)
	hashHex := hex.EncodeToString(payloadHash[:])
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", hashHex)
	headers := []string{"content-type", "host", "x-amz-content-sha256", "x-amz-date", "x-amz-target"}
	canonical := "content-type:" + req.Header.Get("Content-Type") + "\n" +
		"host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + hashHex + "\n" +
		"x-amz-date:" + amzDate + "\n" +
		"x-amz-target:" + req.Header.Get("X-Amz-Target") + "\n"
	if config.AWSSessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", config.AWSSessionToken)
		headers = append(headers, "x-amz-security-token")
		canonical += "x-amz-security-token:" + config.AWSSessionToken + "\n"
	}
	sort.Strings(headers)
	signedHeaders := strings.Join(headers, ";")
	canonicalRequest := strings.Join([]string{
		http.MethodPost, req.URL.EscapedPath(), "", canonical, signedHeaders, hashHex,
	}, "\n")
	scope := date + "/" + config.AWSRegion + "/" + service + "/aws4_request"
	requestHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(requestHash[:])
	key := hmacSHA256Bytes([]byte("AWS4"+secretKey), date)
	key = hmacSHA256Bytes(key, config.AWSRegion)
	key = hmacSHA256Bytes(key, service)
	key = hmacSHA256Bytes(key, "aws4_request")
	signature := hex.EncodeToString(hmacSHA256Bytes(key, stringToSign))
	req.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+config.AWSAccessKey+"/"+scope+
		", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func hmacSHA256Bytes(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

type gcpServiceAccount struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

func (s *Service) postGCP(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, project *store.Project, env *store.Environment, values map[string]string) error {
	rawAccount, err := s.decryptOrgSecret(org, sync.ID, "gcp_service_account", config.GCPServiceAccount)
	if err != nil {
		return err
	}
	var account gcpServiceAccount
	if err := json.Unmarshal([]byte(rawAccount), &account); err != nil || account.ClientEmail == "" || account.PrivateKey == "" {
		return fmt.Errorf("gcp: service account JSON must include client_email and private_key")
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	accessToken, err := gcpAccessToken(ctx, s.WebhookClient, account)
	if err != nil {
		return err
	}
	base := config.BaseURL
	if base == "" {
		base = "https://secretmanager.googleapis.com"
	}
	parent := "projects/" + url.PathEscape(config.GCPProject)
	prefix := config.GCPPrefix
	if prefix == "" {
		prefix = "clavis-" + project.Slug + "-" + env.Slug
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		name := prefix + "-" + sanitizeGCPName(key)
		secretURL := base + "/v1/" + parent + "/secrets/" + url.PathEscape(name)
		status, body, err := gcpRequest(ctx, s.WebhookClient, http.MethodGet, secretURL, accessToken, nil)
		if err != nil {
			return fmt.Errorf("gcp: request failed: %v; check the API URL and network policy", err)
		}
		if status == http.StatusNotFound {
			payload, _ := json.Marshal(map[string]any{"replication": map[string]any{"automatic": map[string]any{}}})
			status, body, err = gcpRequest(ctx, s.WebhookClient, http.MethodPost,
				base+"/v1/"+parent+"/secrets?secretId="+url.QueryEscape(name), accessToken, payload)
			if err != nil {
				return fmt.Errorf("gcp: create secret failed: %v", err)
			}
			if status < 200 || status >= 300 {
				return gcpStatusError("create "+name, status, body)
			}
		} else if status < 200 || status >= 300 {
			return gcpStatusError("read "+name, status, body)
		}
		payload, _ := json.Marshal(map[string]any{
			"payload": map[string]string{"data": base64.StdEncoding.EncodeToString([]byte(values[key]))},
		})
		status, body, err = gcpRequest(ctx, s.WebhookClient, http.MethodPost, secretURL+":addVersion", accessToken, payload)
		if err != nil {
			return fmt.Errorf("gcp: add version failed: %v; check the API URL and network policy", err)
		}
		if status < 200 || status >= 300 {
			return gcpStatusError("add version "+name, status, body)
		}
	}
	return nil
}

func gcpAccessToken(ctx context.Context, client *http.Client, account gcpServiceAccount) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	block, _ := pem.Decode([]byte(account.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("gcp: invalid service account private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if rsaKey, rsaErr := x509.ParsePKCS1PrivateKey(block.Bytes); rsaErr == nil {
			key = rsaKey
		} else {
			return "", fmt.Errorf("gcp: invalid service account private key")
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("gcp: service account key must be RSA")
	}
	now := time.Now().Unix()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss":   account.ClientEmail,
		"scope": "https://www.googleapis.com/auth/cloud-platform",
		"aud":   account.TokenURI, "iat": now, "exp": now + 3600,
	})
	payload := base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(header + "." + payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	assertion := header + "." + payload + "." + base64.RawURLEncoding.EncodeToString(signature)
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	form.Set("assertion", assertion)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("gcp: token exchange failed: %v; check the token URI and network policy", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("gcp: token exchange returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &token); err != nil || token.AccessToken == "" {
		return "", fmt.Errorf("gcp: token response did not include an access token")
	}
	return token.AccessToken, nil
}

func gcpRequest(ctx context.Context, client *http.Client, method, endpoint, token string, payload []byte) (int, []byte, error) {
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	reader := bytes.NewReader(nil)
	if payload != nil {
		reader = bytes.NewReader(payload)
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

func gcpStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("gcp: permission denied for %s; check the service account roles (Secret Manager Admin)", operation)
	case http.StatusNotFound:
		return fmt.Errorf("gcp: %s not found; check the project ID", operation)
	default:
		return fmt.Errorf("gcp: %s returned %d: %s", operation, status, detail)
	}
}

func sanitizeGCPName(key string) string {
	var b strings.Builder
	for _, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	name := b.String()
	if name == "" {
		return "value"
	}
	return name
}

func (s *Service) postNetlify(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, env *store.Environment, values, kinds map[string]string) error {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	query := ""
	if config.NetlifySite != "" {
		query = "?site_id=" + url.QueryEscape(config.NetlifySite)
	}
	envURL := base + "/api/v1/accounts/" + url.PathEscape(config.NetlifyAccount) + "/env"
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, envURL+query, token, nil)
	if err != nil {
		return fmt.Errorf("netlify: could not list variables: %v; check the API URL and network policy", err)
	}
	existing := map[string]bool{}
	if status == http.StatusOK {
		var listing []struct {
			Key string `json:"key"`
		}
		if json.Unmarshal(body, &listing) == nil {
			for _, item := range listing {
				existing[item.Key] = true
			}
		}
	} else {
		return netlifyStatusError("list variables", status, body)
	}
	contextName := "deploy-preview"
	switch env.Type {
	case "production":
		contextName = "production"
	case "development":
		contextName = "dev"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		payload, _ := json.Marshal(map[string]any{
			"key":       key,
			"scopes":    []string{"builds", "functions", "runtime"},
			"values":    []map[string]string{{"value": values[key], "context": contextName}},
			"is_secret": kinds[key] != "variable",
		})
		method := http.MethodPost
		endpoint := envURL + query
		if existing[key] {
			method = http.MethodPut
			endpoint = envURL + "/" + url.PathEscape(key) + query
		}
		status, body, err := githubLikeRequest(ctx, client, method, endpoint, token, payload)
		if err != nil {
			return fmt.Errorf("netlify: request failed: %v; check the API URL and network policy", err)
		}
		if status < 200 || status >= 300 {
			return netlifyStatusError("set "+key, status, body)
		}
	}
	if config.DeleteMissing {
		for remoteKey := range existing {
			if _, keep := values[remoteKey]; keep {
				continue
			}
			status, body, err := githubLikeRequest(ctx, client, http.MethodDelete,
				envURL+"/"+url.PathEscape(remoteKey)+query, token, nil)
			if err != nil {
				return fmt.Errorf("netlify: delete %s failed: %v", remoteKey, err)
			}
			if status >= 300 && status != http.StatusNotFound {
				return netlifyStatusError("delete "+remoteKey, status, body)
			}
		}
	}
	return nil
}

func netlifyStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("netlify: token rejected; check the personal access token")
	case http.StatusForbidden:
		return fmt.Errorf("netlify: permission denied for %s; check account/site access", operation)
	case http.StatusNotFound:
		return fmt.Errorf("netlify: %s not found; check the account slug and site ID", operation)
	default:
		return fmt.Errorf("netlify: %s returned %d: %s", operation, status, detail)
	}
}

func (s *Service) postCloudflare(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, env *store.Environment, values, kinds map[string]string) error {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	accountURL := base + "/client/v4/accounts/" + url.PathEscape(config.CFAccount)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if config.CFMode == "workers" {
		endpoint := accountURL + "/workers/scripts/" + url.PathEscape(config.CFScript) + "/secrets"
		for _, key := range keys {
			var buffer bytes.Buffer
			writer := multipart.NewWriter(&buffer)
			_ = writer.WriteField("name", key)
			_ = writer.WriteField("text", values[key])
			_ = writer.WriteField("type", "secret_text")
			_ = writer.Close()
			request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, &buffer)
			if err != nil {
				return err
			}
			request.Header.Set("Authorization", "Bearer "+token)
			request.Header.Set("Content-Type", writer.FormDataContentType())
			response, err := client.Do(request)
			if err != nil {
				return fmt.Errorf("cloudflare: request failed: %v; check the API URL and network policy", err)
			}
			body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				return cloudflareStatusError("set "+key, response.StatusCode, body)
			}
			var result struct {
				Success bool `json:"success"`
			}
			if json.Unmarshal(body, &result) == nil && !result.Success {
				return fmt.Errorf("cloudflare: set %s was rejected: %s", key, strings.TrimSpace(string(body)))
			}
		}
		return nil
	}

	projectURL := accountURL + "/pages/projects/" + url.PathEscape(config.CFProject)
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, projectURL, token, nil)
	if err != nil {
		return fmt.Errorf("cloudflare: could not read Pages project: %v", err)
	}
	if status < 200 || status >= 300 {
		return cloudflareStatusError("read Pages project", status, body)
	}
	var project struct {
		Result struct {
			DeploymentConfigs map[string]struct {
				EnvVars map[string]struct {
					Value string `json:"value"`
					Type  string `json:"type"`
				} `json:"env_vars"`
			} `json:"deployment_configs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &project); err != nil {
		return fmt.Errorf("cloudflare: invalid Pages project response")
	}
	target := "preview"
	if env.Type == "production" {
		target = "production"
	}
	targetConfig := project.Result.DeploymentConfigs[target]
	if targetConfig.EnvVars == nil {
		targetConfig.EnvVars = map[string]struct {
			Value string `json:"value"`
			Type  string `json:"type"`
		}{}
	}
	for _, key := range keys {
		valueType := "secret_text"
		if kinds[key] == "variable" {
			valueType = "plain_text"
		}
		targetConfig.EnvVars[key] = struct {
			Value string `json:"value"`
			Type  string `json:"type"`
		}{Value: values[key], Type: valueType}
	}
	patch, _ := json.Marshal(map[string]any{
		"deployment_configs": map[string]any{
			target: map[string]any{"env_vars": targetConfig.EnvVars},
		},
	})
	status, body, err = githubLikeRequest(ctx, client, http.MethodPatch, projectURL, token, patch)
	if err != nil {
		return fmt.Errorf("cloudflare: Pages update failed: %v", err)
	}
	if status < 200 || status >= 300 {
		return cloudflareStatusError("update Pages project", status, body)
	}
	return nil
}

func cloudflareStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("cloudflare: token rejected; check the API token")
	case http.StatusForbidden:
		return fmt.Errorf("cloudflare: permission denied for %s; check account and Workers/Pages permissions", operation)
	case http.StatusNotFound:
		return fmt.Errorf("cloudflare: %s not found; check the account ID, script, or Pages project", operation)
	default:
		return fmt.Errorf("cloudflare: %s returned %d: %s", operation, status, detail)
	}
}

// SyncDrift is the result of comparing local values to a provider.
type SyncDrift struct {
	Supported bool
	Keys      []string
	CheckedAt time.Time
	Detail    string
}

// CheckSyncDrift compares the environment's effective values with the remote
// provider where the provider exposes readable values.
func (s *Service) CheckSyncDrift(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, id string) (*SyncDrift, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	sync, err := s.Store.GetSync(ctx, project.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	env, err := s.Store.GetEnvironmentByID(ctx, sync.EnvironmentID)
	if err != nil {
		return nil, err
	}
	var config syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
		return nil, err
	}
	values, kinds, err := s.syncValues(ctx, org, project, env)
	if err != nil {
		return nil, err
	}
	now := s.Now().UTC()
	result := &SyncDrift{CheckedAt: now, Keys: []string{}}
	var keys []string
	switch sync.Provider {
	case "gitlab":
		keys, err = s.driftGitLab(ctx, org, sync, config, values)
		result.Detail = "GitLab variable values compared"
	case "aws":
		keys, err = s.driftAWS(ctx, org, sync, config, project, env, values)
		result.Detail = "AWS values compared"
	case "gcp":
		keys, err = s.driftGCP(ctx, org, sync, config, project, env, values)
		result.Detail = "GCP secret versions compared"
	case "netlify":
		keys, err = s.driftNetlify(ctx, org, sync, config, env, values)
		result.Detail = "Netlify variables compared"
	case "vercel":
		keys, err = s.driftVercel(ctx, org, sync, config, values)
		result.Detail = "Vercel variables compared"
	case "github":
		keys, err = s.driftGitHub(ctx, org, sync, config, values, kinds)
		result.Detail = "GitHub variables compared; secrets checked for presence"
	case "cloudflare":
		if config.CFMode == "pages" {
			keys, err = s.driftCloudflare(ctx, org, sync, config, env, values)
			result.Detail = "Cloudflare Pages variables compared"
		} else {
			result.Detail = "Cloudflare Workers secrets are write-only"
		}
	default:
		result.Detail = "Drift detection is not available for this provider yet"
	}
	if err != nil {
		return nil, err
	}
	if result.Detail != "" && !strings.Contains(result.Detail, "not available") {
		result.Supported = true
		result.Keys = keys
	}
	if result.Keys == nil {
		result.Keys = []string{}
	}
	if result.Supported {
		encoded, _ := json.Marshal(result.Keys)
		if err := s.Store.UpdateSyncDrift(ctx, sync.ID, string(encoded), now); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (s *Service) driftGitLab(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values map[string]string) ([]string, error) {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	endpoint := base + "/api/v4/projects/" + url.PathEscape(config.Project) + "/variables?per_page=100"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("PRIVATE-TOKEN", token)
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("gitlab drift: request failed: %v", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("gitlab drift: list returned %d", response.StatusCode)
	}
	var variables []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(body, &variables); err != nil {
		return nil, fmt.Errorf("gitlab drift: invalid response")
	}
	remote := make(map[string]string, len(variables))
	for _, variable := range variables {
		remote[variable.Key] = variable.Value
	}
	keys := make([]string, 0)
	for key, value := range values {
		remoteValue, ok := remote[key]
		if !ok || remoteValue != value {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// UpdateSyncBehavior toggles provider behavior without recreating the sync.
func (s *Service) UpdateSyncBehavior(ctx context.Context, actor authz.Actor, orgSlug, projectSlug, id string, deleteMissing bool, meta AuditMeta) (*SyncView, error) {
	org, project, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	sync, err := s.Store.GetSync(ctx, project.ID, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var config syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
		return nil, err
	}
	config.DeleteMissing = deleteMissing
	encoded, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	if err := s.Store.UpdateSyncConfig(ctx, project.ID, id, string(encoded)); err != nil {
		return nil, err
	}
	sync.Config = string(encoded)
	env, _ := s.Store.GetEnvironmentByID(ctx, sync.EnvironmentID)
	envSlug := ""
	if env != nil {
		envSlug = env.Slug
	}
	if err := s.record(ctx, toAuditActor(actor), org.ID, "sync.behavior",
		audit.Resource{Type: "sync", ID: sync.ID, ProjectID: project.ID, EnvID: sync.EnvironmentID},
		audit.Details{Environment: envSlug, Reason: fmt.Sprintf("delete_missing=%t", deleteMissing)}, meta); err != nil {
		s.Logger.Error("audit sync.behavior failed", "sync_id", sync.ID, "error", err)
	}
	return &SyncView{Sync: sync, Environment: envSlug}, nil
}
