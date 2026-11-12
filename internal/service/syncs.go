package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

type syncConfig struct {
	Endpoint        string `json:"endpoint,omitempty"`
	Secret          []byte `json:"secret,omitempty"`
	BaseURL         string `json:"base_url,omitempty"`
	Project         string `json:"project,omitempty"`
	Token           []byte `json:"token,omitempty"`
	Protected       bool   `json:"protected,omitempty"`
	Masked          bool   `json:"masked,omitempty"`
	GitHubRepo      string `json:"github_repo,omitempty"`
	GitHubEnv       string `json:"github_environment,omitempty"`
	GitHubScope     string `json:"github_scope,omitempty"`
	VercelProject   string `json:"vercel_project,omitempty"`
	VercelTeamID    string `json:"vercel_team_id,omitempty"`
	AWSRegion       string `json:"aws_region,omitempty"`
	AWSAccessKey    string `json:"aws_access_key,omitempty"`
	AWSSecret       []byte `json:"aws_secret,omitempty"`
	AWSService      string `json:"aws_service,omitempty"`
	AWSPrefix       string `json:"aws_prefix,omitempty"`
	AWSSessionToken string `json:"aws_session_token,omitempty"`
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
	if provider != "http" && provider != "gitlab" && provider != "github" && provider != "vercel" && provider != "aws" {
		return nil, fmt.Errorf("%w: provider must be http, gitlab, github, vercel, or aws", ErrValidation)
	}
	id := uuid.NewString()
	config := syncConfig{Protected: in.Protected, Masked: in.Masked}
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
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-amz-json-1.1")
	request.Header.Set("X-Amz-Target", target)
	signAWSJSON(request, payload, service, config, secretKey)
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("aws: request failed: %v; check the region, endpoint, and network policy", err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return nil
	}
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch response.StatusCode {
	case http.StatusForbidden, http.StatusUnauthorized:
		return fmt.Errorf("aws: credentials rejected; check the access key, secret, and IAM permissions")
	case http.StatusBadRequest:
		return fmt.Errorf("aws: %s: %s", target, detail)
	default:
		return fmt.Errorf("aws: %s returned %d: %s", target, response.StatusCode, detail)
	}
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
