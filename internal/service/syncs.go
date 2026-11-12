package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
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

	"github.com/dumhouse/clavis/internal/audit"
	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// ErrSyncUnconfirmed requires an explicit confirmation before the first push.
var ErrSyncUnconfirmed = errors.New("service: sync requires first-run confirmation")

type syncConfig struct {
	Endpoint  string `json:"endpoint,omitempty"`
	Secret    []byte `json:"secret,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	Project   string `json:"project,omitempty"`
	Token     []byte `json:"token,omitempty"`
	Protected bool   `json:"protected,omitempty"`
	Masked    bool   `json:"masked,omitempty"`
}

// SyncInput creates a sync definition.
type SyncInput struct {
	EnvironmentSlug string
	Provider        string
	Endpoint        string
	Secret          string
	BaseURL         string
	GitLabProject   string
	Token           string
	Protected       bool
	Masked          bool
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
	if provider != "http" && provider != "gitlab" {
		return nil, fmt.Errorf("%w: provider must be http or gitlab", ErrValidation)
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
	values, err := s.syncValues(ctx, org, project, env)
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

func (s *Service) syncValues(ctx context.Context, org *store.Org, project *store.Project, env *store.Environment) (map[string]string, error) {
	valueKey, indexKey, err := s.ProjectKeys(ctx, org, project)
	if err != nil {
		return nil, err
	}
	defer valueKey.Zero()
	defer indexKey.Zero()
	chain, err := s.environmentChain(ctx, env)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var secrets []Secret
	for _, sourceEnv := range chain {
		items, err := s.Store.ListItems(ctx, sourceEnv.ID)
		if err != nil {
			return nil, err
		}
		for i := range items {
			item := &items[i]
			key, err := s.decryptKey(valueKey, org, project, sourceEnv, item)
			if err != nil {
				return nil, err
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			value := ""
			if len(item.ValueCiphertext) > 0 {
				decrypted, err := decryptField(valueKey, org, project, sourceEnv, item, "value", item.Version, item.ValueCiphertext)
				if err != nil {
					return nil, err
				}
				value = string(decrypted)
			}
			secrets = append(secrets, Secret{Key: key, Kind: item.Kind, Value: value})
		}
	}
	if err := resolveSecretReferences(secrets); err != nil {
		return nil, err
	}
	values := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		values[secret.Key] = secret.Value
	}
	return values, nil
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
