package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/store"
)

func (s *Service) driftAWS(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, project *store.Project, env *store.Environment, values map[string]string) ([]string, error) {
	secretKey, err := s.decryptOrgSecret(org, sync.ID, "aws_secret", config.AWSSecret)
	if err != nil {
		return nil, err
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
	keys := make([]string, 0)
	for _, key := range sortedStringKeys(values) {
		name := prefix + "/" + key
		if config.AWSService == "ssm" {
			body, _ := json.Marshal(map[string]any{"Name": name, "WithDecryption": true})
			status, response, err := awsJSONResponse(ctx, client, endpoint, "AmazonSSM.GetParameter", "ssm", config, secretKey, body)
			if err != nil {
				return nil, err
			}
			if status != http.StatusOK {
				keys = append(keys, key)
				continue
			}
			var result struct {
				Parameter struct {
					Value string `json:"Value"`
				} `json:"Parameter"`
			}
			if json.Unmarshal(response, &result) != nil || result.Parameter.Value != values[key] {
				keys = append(keys, key)
			}
			continue
		}
		body, _ := json.Marshal(map[string]any{"SecretId": name})
		status, response, err := awsJSONResponse(ctx, client, endpoint, "secretsmanager.GetSecretValue", "secretsmanager", config, secretKey, body)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			keys = append(keys, key)
			continue
		}
		var result struct {
			SecretString string `json:"SecretString"`
		}
		if json.Unmarshal(response, &result) != nil || result.SecretString != values[key] {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Service) driftGCP(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, project *store.Project, env *store.Environment, values map[string]string) ([]string, error) {
	rawAccount, err := s.decryptOrgSecret(org, sync.ID, "gcp_service_account", config.GCPServiceAccount)
	if err != nil {
		return nil, err
	}
	var account gcpServiceAccount
	if err := json.Unmarshal([]byte(rawAccount), &account); err != nil {
		return nil, err
	}
	if account.TokenURI == "" {
		account.TokenURI = "https://oauth2.googleapis.com/token"
	}
	token, err := gcpAccessToken(ctx, s.WebhookClient, account)
	if err != nil {
		return nil, err
	}
	base := config.BaseURL
	if base == "" {
		base = "https://secretmanager.googleapis.com"
	}
	prefix := config.GCPPrefix
	if prefix == "" {
		prefix = "clavis-" + project.Slug + "-" + env.Slug
	}
	keys := make([]string, 0)
	for _, key := range sortedStringKeys(values) {
		name := prefix + "-" + sanitizeGCPName(key)
		secretURL := base + "/v1/projects/" + url.PathEscape(config.GCPProject) +
			"/secrets/" + url.PathEscape(name) + "/versions/latest:access"
		status, body, err := gcpRequest(ctx, s.WebhookClient, http.MethodGet, secretURL, token, nil)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			keys = append(keys, key)
			continue
		}
		var payload struct {
			Payload struct {
				Data string `json:"data"`
			} `json:"payload"`
		}
		if json.Unmarshal(body, &payload) != nil {
			keys = append(keys, key)
			continue
		}
		decoded, _ := base64.StdEncoding.DecodeString(payload.Payload.Data)
		if string(decoded) != values[key] {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func (s *Service) driftNetlify(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, env *store.Environment, values map[string]string) ([]string, error) {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
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
	endpoint := base + "/api/v1/accounts/" + url.PathEscape(config.NetlifyAccount) + "/env" + query
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, endpoint, token, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, netlifyStatusError("drift list", status, body)
	}
	var listing []struct {
		Key    string `json:"key"`
		Values []struct {
			Value   string `json:"value"`
			Context string `json:"context"`
		} `json:"values"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}
	contextName := "deploy-preview"
	switch env.Type {
	case "production":
		contextName = "production"
	case "development":
		contextName = "dev"
	}
	remote := map[string]string{}
	for _, item := range listing {
		for _, value := range item.Values {
			if value.Context == contextName || value.Context == "all" {
				remote[item.Key] = value.Value
				break
			}
		}
	}
	keys := make([]string, 0)
	for key, value := range values {
		if remote[key] != value {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Service) driftVercel(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values map[string]string) ([]string, error) {
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	project := url.PathEscape(config.VercelProject)
	query := "?decrypt=true"
	if config.VercelTeamID != "" {
		query += "&teamId=" + url.QueryEscape(config.VercelTeamID)
	}
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet,
		base+"/v10/projects/"+project+"/env"+query, token, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, vercelStatusError("drift list", status, body)
	}
	var listing struct {
		Envs []struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		} `json:"envs"`
	}
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, err
	}
	remote := map[string]string{}
	for _, item := range listing.Envs {
		remote[item.Key] = item.Value
	}
	keys := make([]string, 0)
	for key, value := range values {
		if remote[key] != value {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Service) driftCloudflare(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, env *store.Environment, values map[string]string) ([]string, error) {
	if config.CFMode == "workers" {
		return nil, nil
	}
	token, err := s.decryptOrgSecret(org, sync.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	client := s.WebhookClient
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	base := strings.TrimRight(config.BaseURL, "/")
	projectURL := base + "/client/v4/accounts/" + url.PathEscape(config.CFAccount) +
		"/pages/projects/" + url.PathEscape(config.CFProject)
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, projectURL, token, nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, cloudflareStatusError("drift read", status, body)
	}
	var project struct {
		Result struct {
			DeploymentConfigs map[string]struct {
				EnvVars map[string]struct {
					Value string `json:"value"`
				} `json:"env_vars"`
			} `json:"deployment_configs"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &project); err != nil {
		return nil, err
	}
	target := "preview"
	if env.Type == "production" {
		target = "production"
	}
	remote := project.Result.DeploymentConfigs[target].EnvVars
	keys := make([]string, 0)
	for key, value := range values {
		item, ok := remote[key]
		if !ok || (item.Value != "" && item.Value != value) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (s *Service) driftGitHub(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values, kinds map[string]string) ([]string, error) {
	token, err := s.githubSyncToken(ctx, org, sync, config)
	if err != nil {
		return nil, err
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
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, actions+"/variables?per_page=100", token, nil)
	if err != nil {
		return nil, err
	}
	remoteVariables := map[string]string{}
	if status == http.StatusOK {
		var listing struct {
			Variables []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"variables"`
		}
		if json.Unmarshal(body, &listing) == nil {
			for _, item := range listing.Variables {
				remoteVariables[item.Name] = item.Value
			}
		}
	}
	status, body, err = githubLikeRequest(ctx, client, http.MethodGet, actions+"/secrets?per_page=100", token, nil)
	if err != nil {
		return nil, err
	}
	remoteSecrets := map[string]bool{}
	if status == http.StatusOK {
		var listing struct {
			Secrets []struct {
				Name string `json:"name"`
			} `json:"secrets"`
		}
		if json.Unmarshal(body, &listing) == nil {
			for _, item := range listing.Secrets {
				remoteSecrets[item.Name] = true
			}
		}
	}
	keys := make([]string, 0)
	for key, value := range values {
		if kinds[key] == "variable" {
			if remoteVariables[key] != value {
				keys = append(keys, key)
			}
		} else if !remoteSecrets[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
