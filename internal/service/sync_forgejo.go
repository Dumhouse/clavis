package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/store"
)

// Forgejo (and Gitea, which shares the API) Actions secrets and variables.
//
//	PUT    /api/v1/repos/{owner}/{repo}/actions/secrets/{name}    {"data": value}
//	GET    /api/v1/repos/{owner}/{repo}/actions/secrets           names only
//	GET    /api/v1/repos/{owner}/{repo}/actions/variables         names and values
//	POST   /api/v1/repos/{owner}/{repo}/actions/variables/{name}  {"value": value}
//	PUT    /api/v1/repos/{owner}/{repo}/actions/variables/{name}  {"name": name, "value": value}
//
// The same paths exist under /api/v1/orgs/{org}/actions for organization scope.
// Forgejo stores names upper-cased, so names are compared case-insensitively.

const forgejoDefaultURL = "https://codeberg.org"

func validateForgejoConfig(config *syncConfig, in SyncInput, inline bool) error {
	config.ForgejoRepo = strings.Trim(strings.TrimSpace(in.ForgejoRepo), "/")
	config.ForgejoScope = strings.ToLower(strings.TrimSpace(in.ForgejoScope))
	if config.ForgejoScope == "" {
		config.ForgejoScope = "repo"
	}
	switch config.ForgejoScope {
	case "repo":
		parts := strings.Split(config.ForgejoRepo, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return fmt.Errorf("%w: Forgejo repository must be owner/name", ErrValidation)
		}
	case "org":
		if config.ForgejoRepo == "" || strings.Contains(config.ForgejoRepo, "/") {
			return fmt.Errorf("%w: Forgejo organization sync needs an organization name", ErrValidation)
		}
	default:
		return fmt.Errorf("%w: Forgejo scope must be repo or org", ErrValidation)
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if config.BaseURL == "" {
		config.BaseURL = forgejoDefaultURL
	}
	config.BaseURL = strings.TrimSuffix(config.BaseURL, "/api/v1")
	if !strings.HasPrefix(config.BaseURL, "https://") && !strings.HasPrefix(config.BaseURL, "http://") {
		return fmt.Errorf("%w: Forgejo URL must be http(s)", ErrValidation)
	}
	if inline && in.Token == "" {
		return fmt.Errorf("%w: Forgejo access token is required", ErrValidation)
	}
	return nil
}

func forgejoActionsBase(config syncConfig) string {
	base := strings.TrimRight(config.BaseURL, "/") + "/api/v1"
	if config.ForgejoScope == "org" {
		return base + "/orgs/" + url.PathEscape(config.ForgejoRepo) + "/actions"
	}
	parts := strings.SplitN(config.ForgejoRepo, "/", 2)
	return base + "/repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/actions"
}

func forgejoDestination(config syncConfig) string {
	return "forgejo:" + config.BaseURL + " " + config.ForgejoScope + ":" + config.ForgejoRepo
}

type forgejoEntry struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

func (s *Service) forgejoClient() *http.Client {
	if s.WebhookClient != nil {
		return s.WebhookClient
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (s *Service) postForgejo(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values, kinds map[string]string) error {
	token, err := s.decryptOrgSecret(org, syncCredentialOwner(sync), "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.forgejoClient()
	actions := forgejoActionsBase(config)

	keys := sortedStringKeys(values)
	for _, name := range keys {
		if !validForgejoName(name) {
			return fmt.Errorf("forgejo: %q is not a valid Actions name (letters, digits, underscore; cannot start with a digit, GITHUB_, GITEA_, or FORGEJO_)", name)
		}
	}

	var variables []forgejoEntry
	hasVariables := false
	for _, name := range keys {
		if kinds[name] == "variable" {
			hasVariables = true
			break
		}
	}
	if hasVariables || config.DeleteMissing {
		variables, err = forgejoList(ctx, client, actions+"/variables", token)
		if err != nil {
			return err
		}
	}
	existingVariables := map[string]bool{}
	for _, variable := range variables {
		existingVariables[strings.ToUpper(variable.Name)] = true
	}

	for _, name := range keys {
		value := values[name]
		if kinds[name] == "variable" {
			method := http.MethodPost
			payload, _ := json.Marshal(map[string]string{"value": value})
			if existingVariables[strings.ToUpper(name)] {
				method = http.MethodPut
				payload, _ = json.Marshal(map[string]string{"name": name, "value": value})
			}
			status, body, err := forgejoRequest(ctx, client, method, actions+"/variables/"+url.PathEscape(name), token, payload)
			if err != nil {
				return fmt.Errorf("forgejo: variable %s failed: %w; check the Forgejo URL and network policy", name, err)
			}
			if status < 200 || status >= 300 {
				return forgejoStatusError("variable "+name, status, body)
			}
			continue
		}
		payload, _ := json.Marshal(map[string]string{"data": value})
		status, body, err := forgejoRequest(ctx, client, http.MethodPut, actions+"/secrets/"+url.PathEscape(name), token, payload)
		if err != nil {
			return fmt.Errorf("forgejo: secret %s failed: %w; check the Forgejo URL and network policy", name, err)
		}
		if status < 200 || status >= 300 {
			return forgejoStatusError("secret "+name, status, body)
		}
	}

	if !config.DeleteMissing {
		return nil
	}
	keep := map[string]bool{}
	for name := range values {
		keep[strings.ToUpper(name)] = true
	}
	for _, variable := range variables {
		if keep[strings.ToUpper(variable.Name)] {
			continue
		}
		status, body, err := forgejoRequest(ctx, client, http.MethodDelete, actions+"/variables/"+url.PathEscape(variable.Name), token, nil)
		if err != nil {
			return fmt.Errorf("forgejo: delete variable %s failed: %w", variable.Name, err)
		}
		if status >= 300 && status != http.StatusNotFound {
			return forgejoStatusError("delete variable "+variable.Name, status, body)
		}
	}
	secrets, err := forgejoList(ctx, client, actions+"/secrets", token)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if keep[strings.ToUpper(secret.Name)] {
			continue
		}
		status, body, err := forgejoRequest(ctx, client, http.MethodDelete, actions+"/secrets/"+url.PathEscape(secret.Name), token, nil)
		if err != nil {
			return fmt.Errorf("forgejo: delete secret %s failed: %w", secret.Name, err)
		}
		if status >= 300 && status != http.StatusNotFound {
			return forgejoStatusError("delete secret "+secret.Name, status, body)
		}
	}
	return nil
}

// driftForgejo compares variable values and checks that every secret exists;
// secret values are write-only and cannot be compared.
func (s *Service) driftForgejo(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values, kinds map[string]string) ([]string, error) {
	token, err := s.decryptOrgSecret(org, syncCredentialOwner(sync), "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	client := s.forgejoClient()
	actions := forgejoActionsBase(config)
	variables, err := forgejoList(ctx, client, actions+"/variables", token)
	if err != nil {
		return nil, err
	}
	secrets, err := forgejoList(ctx, client, actions+"/secrets", token)
	if err != nil {
		return nil, err
	}
	remoteVariables := map[string]string{}
	for _, variable := range variables {
		remoteVariables[strings.ToUpper(variable.Name)] = variable.Data
	}
	remoteSecrets := map[string]bool{}
	for _, secret := range secrets {
		remoteSecrets[strings.ToUpper(secret.Name)] = true
	}
	keys := make([]string, 0)
	for key, value := range values {
		upper := strings.ToUpper(key)
		if kinds[key] == "variable" {
			if remote, ok := remoteVariables[upper]; !ok || remote != value {
				keys = append(keys, key)
			}
		} else if !remoteSecrets[upper] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

// forgejoList reads a paginated Actions listing.
func forgejoList(ctx context.Context, client *http.Client, endpoint, token string) ([]forgejoEntry, error) {
	const pageSize = 50
	var all []forgejoEntry
	for page := 1; page <= 40; page++ {
		status, body, err := forgejoRequest(ctx, client, http.MethodGet,
			fmt.Sprintf("%s?page=%d&limit=%d", endpoint, page, pageSize), token, nil)
		if err != nil {
			return nil, fmt.Errorf("forgejo: list failed: %w; check the Forgejo URL and network policy", err)
		}
		if status == http.StatusNotFound && page == 1 && strings.HasSuffix(endpoint, "/variables") {
			// Older Forgejo releases have no variables API; treat as empty.
			return nil, nil
		}
		if status < 200 || status >= 300 {
			return nil, forgejoStatusError("list", status, body)
		}
		var entries []forgejoEntry
		if err := json.Unmarshal(body, &entries); err != nil {
			return nil, fmt.Errorf("forgejo: invalid list response")
		}
		all = append(all, entries...)
		if len(entries) < pageSize {
			break
		}
	}
	return all, nil
}

func forgejoRequest(ctx context.Context, client *http.Client, method, endpoint, token string, payload []byte) (int, []byte, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Authorization", "token "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "clavis-sync/1")
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	return response.StatusCode, body, nil
}

func forgejoStatusError(operation string, status int, body []byte) error {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 300 {
		detail = detail[:300]
	}
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("forgejo: token rejected; check that it is valid and not expired")
	case http.StatusForbidden:
		return fmt.Errorf("forgejo: permission denied for %s; the token needs write:repository (or write:organization for org scope)", operation)
	case http.StatusNotFound:
		return fmt.Errorf("forgejo: %s not found; check the repository or organization, that Actions is enabled, and the Forgejo URL", operation)
	default:
		return fmt.Errorf("forgejo: %s returned %d: %s", operation, status, detail)
	}
}

func validForgejoName(name string) bool {
	upper := strings.ToUpper(name)
	if name == "" || strings.HasPrefix(upper, "GITHUB_") || strings.HasPrefix(upper, "GITEA_") || strings.HasPrefix(upper, "FORGEJO_") {
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
