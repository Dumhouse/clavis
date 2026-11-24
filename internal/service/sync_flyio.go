package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/dumhouse/clavis/internal/store"
)

// Fly.io app secrets through the Machines API.
//
//	POST /v1/apps/{app}/secrets  {"values": {"NAME": "value"}}  null deletes
//	GET  /v1/apps/{app}/secrets  names and digests, never values
//
// Running Machines keep the values they booted with until they are updated
// with the secrets version returned by the API, so a sync does not restart or
// redeploy the app.

const flyioDefaultURL = "https://api.machines.dev"

func validateFlyioConfig(config *syncConfig, in SyncInput, inline bool) error {
	config.FlyioApp = strings.TrimSpace(in.FlyioApp)
	if !validFlyioApp(config.FlyioApp) {
		return fmt.Errorf("%w: Fly.io app must be 3-30 characters: lowercase letters, digits, and hyphens, starting with a letter", ErrValidation)
	}
	config.BaseURL = strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	if config.BaseURL == "" {
		config.BaseURL = flyioDefaultURL
	}
	if !strings.HasPrefix(config.BaseURL, "https://") && !strings.HasPrefix(config.BaseURL, "http://") {
		return fmt.Errorf("%w: Fly.io API URL must be http(s)", ErrValidation)
	}
	if inline && in.Token == "" {
		return fmt.Errorf("%w: Fly.io API token is required", ErrValidation)
	}
	return nil
}

func validFlyioApp(name string) bool {
	if len(name) < 3 || len(name) > 30 {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case i > 0 && r >= '0' && r <= '9':
		case i > 0 && i < len(name)-1 && r == '-':
		default:
			return false
		}
	}
	return true
}

func validFlyioSecretName(name string) bool {
	if name == "" {
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

func flyioDestination(config syncConfig) string {
	return "flyio:" + config.FlyioApp
}

func flyioSecretsURL(config syncConfig) string {
	return strings.TrimRight(config.BaseURL, "/") + "/v1/apps/" + url.PathEscape(config.FlyioApp) + "/secrets"
}

func (s *Service) postFlyio(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values map[string]string) error {
	token, err := s.decryptOrgSecret(org, syncCredentialOwner(sync), "sync_token", config.Token)
	if err != nil {
		return err
	}
	client := s.forgejoClient()

	keys := sortedStringKeys(values)
	for _, name := range keys {
		if !validFlyioSecretName(name) {
			return fmt.Errorf("flyio: %q is not a valid Fly secret name (letters, digits, and underscores; cannot start with a digit)", name)
		}
	}

	updates := make(map[string]*string, len(keys))
	for _, name := range keys {
		value := values[name]
		updates[name] = &value
	}
	if config.DeleteMissing {
		existing, err := flyioListSecrets(ctx, client, config, token)
		if err != nil {
			return err
		}
		for _, name := range existing {
			if _, keep := values[name]; !keep {
				updates[name] = nil
			}
		}
	}
	if len(updates) == 0 {
		return nil
	}
	payload, err := json.Marshal(map[string]any{"values": updates})
	if err != nil {
		return err
	}
	status, body, err := githubLikeRequest(ctx, client, http.MethodPost, flyioSecretsURL(config), token, payload)
	if err != nil {
		return fmt.Errorf("flyio: set secrets failed: %w; check the Fly.io API and network policy", err)
	}
	if status < 200 || status >= 300 {
		return flyioStatusError("set secrets", status, body)
	}
	return nil
}

// driftFlyio checks that every key exists. Fly never returns secret values
// outside the app's own Machines, so only presence can be compared.
func (s *Service) driftFlyio(ctx context.Context, org *store.Org, sync *store.Sync, config syncConfig, values map[string]string) ([]string, error) {
	token, err := s.decryptOrgSecret(org, syncCredentialOwner(sync), "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	existing, err := flyioListSecrets(ctx, s.forgejoClient(), config, token)
	if err != nil {
		return nil, err
	}
	remote := make(map[string]bool, len(existing))
	for _, name := range existing {
		remote[name] = true
	}
	var keys []string
	for name := range values {
		if !remote[name] {
			keys = append(keys, name)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func flyioListSecrets(ctx context.Context, client *http.Client, config syncConfig, token string) ([]string, error) {
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, flyioSecretsURL(config), token, nil)
	if err != nil {
		return nil, fmt.Errorf("flyio: list secrets failed: %w; check the Fly.io API and network policy", err)
	}
	if status < 200 || status >= 300 {
		return nil, flyioStatusError("list secrets", status, body)
	}
	var result struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("flyio: invalid secrets response")
	}
	names := make([]string, 0, len(result.Secrets))
	for _, secret := range result.Secrets {
		if secret.Name != "" {
			names = append(names, secret.Name)
		}
	}
	return names, nil
}

func flyioStatusError(operation string, status int, body []byte) error {
	switch status {
	case http.StatusUnauthorized:
		return fmt.Errorf("flyio: token rejected; create a Fly.io API token and check that it is not expired")
	case http.StatusForbidden:
		return fmt.Errorf("flyio: permission denied for %s; the token needs access to this app", operation)
	case http.StatusNotFound:
		return fmt.Errorf("flyio: %s: app not found; check the app name and that the token can see it", operation)
	case http.StatusBadRequest:
		return fmt.Errorf("flyio: %s rejected: %s", operation, trimDetail(body))
	default:
		return fmt.Errorf("flyio: %s returned %d: %s", operation, status, trimDetail(body))
	}
}

// testFlyioConnection checks an app the connection's syncs push to. An
// app-scoped deploy token cannot list every app, so a bare token check would
// report a false failure.
func (s *Service) testFlyioConnection(ctx context.Context, org *store.Org, conn *store.SyncConnection, config syncConfig) (*ConnectionTest, error) {
	token, err := s.decryptOrgSecret(org, conn.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
	}
	client := s.forgejoClient()
	base := conn.BaseURL
	if base == "" {
		base = flyioDefaultURL
	}
	checkApp := func(app string) ConnectionCheck {
		target := ConnectionCheck{Target: "App " + app}
		secrets := flyioSecretsURL(syncConfig{BaseURL: base, FlyioApp: app})
		status, body, err := githubLikeRequest(ctx, client, http.MethodGet, secrets, token, nil)
		switch {
		case err != nil:
			target.Detail = fmt.Sprintf("Request failed: %v.", err)
		case status >= 200 && status < 300:
			target.OK = true
			target.Detail = "Can manage secrets in " + app + "."
		case status == http.StatusUnauthorized:
			target.Detail = "Fly.io rejected the token: it is wrong, expired, or was revoked. Create a new one and rotate this connection."
		case status == http.StatusForbidden:
			target.Detail = "The token cannot manage secrets in " + app + ". Use a token for this app, or an organization token that can deploy it."
		case status == http.StatusNotFound:
			target.Detail = "App " + app + " was not found. Check the name and the token's access."
		default:
			target.Detail = fmt.Sprintf("Fly.io returned HTTP %d: %s", status, trimDetail(body))
		}
		return target
	}

	usage, err := s.Store.ListSyncConnectionUsage(ctx, org.ID, conn.ID)
	if err != nil {
		return nil, err
	}
	var apps []string
	seen := map[string]bool{}
	for _, item := range usage[conn.ID] {
		sync, err := s.Store.GetSync(ctx, item.ProjectID, item.SyncID)
		if err != nil {
			continue
		}
		var config syncConfig
		if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
			continue
		}
		if config.FlyioApp == "" || seen[config.FlyioApp] || len(apps) >= 5 {
			continue
		}
		seen[config.FlyioApp] = true
		apps = append(apps, config.FlyioApp)
	}
	if len(apps) > 0 {
		result := &ConnectionTest{}
		for _, app := range apps {
			result.Checks = append(result.Checks, checkApp(app))
		}
		result.OK = true
		for _, check := range result.Checks {
			if !check.OK {
				result.OK = false
				if result.Detail == "" {
					result.Detail = check.Target + ": " + check.Detail
				}
			}
		}
		if result.OK {
			if len(apps) == 1 {
				result.Detail = "Can manage secrets in " + apps[0] + "."
			} else {
				result.Detail = fmt.Sprintf("Can manage secrets in all %d apps.", len(apps))
			}
		}
		return result, nil
	}

	target := ConnectionCheck{Target: "Token"}
	status, body, err := githubLikeRequest(ctx, client, http.MethodGet, strings.TrimRight(base, "/")+"/v1/apps", token, nil)
	switch {
	case err != nil:
		target.Detail = fmt.Sprintf("Cannot reach %s: %v. Check the Fly.io API URL and network policy.", base, err)
	case status == http.StatusUnauthorized:
		target.Detail = "Fly.io rejected the token: it is wrong, expired, or was revoked."
	case status >= 200 && status < 300:
		var apps struct {
			Total int `json:"total_apps"`
		}
		_ = json.Unmarshal(body, &apps)
		target.OK = true
		target.Detail = "Token is valid. No sync uses this connection yet, so app access was not checked; syncs check it when they run."
		if apps.Total > 0 {
			target.Detail = fmt.Sprintf("Token is valid for %d app(s). No sync uses this connection yet, so app access was not checked.", apps.Total)
		}
	case status == http.StatusForbidden:
		target.Detail = "This looks like an app-scoped token. Create a sync with this connection to check that app, or use an organization token."
	default:
		target.Detail = fmt.Sprintf("Fly.io returned HTTP %d: %s", status, trimDetail(body))
	}
	return &ConnectionTest{Checks: []ConnectionCheck{target}, OK: target.OK, Detail: target.Detail}, nil
}
