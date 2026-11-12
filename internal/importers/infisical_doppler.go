package importers

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

func loadInfisical(ctx context.Context, opts Options) ([]Entry, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, fmt.Errorf("Infisical token is required (--token or INFISICAL_TOKEN)")
	}
	if strings.TrimSpace(opts.RemoteProject) == "" || strings.TrimSpace(opts.RemoteEnv) == "" {
		return nil, fmt.Errorf("Infisical project and environment are required")
	}
	base, err := normalizeBase(opts.BaseURL, "https://app.infisical.com")
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("workspaceId", opts.RemoteProject)
	query.Set("environment", opts.RemoteEnv)
	secretPath := opts.RemotePath
	if secretPath == "" {
		secretPath = "/"
	}
	query.Set("secretPath", secretPath)
	endpoint := base + "/api/v3/secrets/raw?" + query.Encode()
	var response struct {
		Secrets []struct {
			SecretKey   string `json:"secretKey"`
			SecretValue string `json:"secretValue"`
			Type        string `json:"type"`
		} `json:"secrets"`
	}
	if err := getJSON(ctx, opts, endpoint, opts.Token, &response); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(response.Secrets))
	for _, secret := range response.Secrets {
		if secret.SecretKey == "" {
			continue
		}
		kind := "secret"
		if strings.EqualFold(secret.Type, "shared") && strings.Contains(secret.SecretValue, "{") {
			kind = "variable"
		}
		entries = append(entries, Entry{Key: secret.SecretKey, Value: secret.SecretValue, Kind: kind, Source: "infisical"})
	}
	return entries, nil
}

func loadDoppler(ctx context.Context, opts Options) ([]Entry, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, fmt.Errorf("Doppler token is required (--token or DOPPLER_TOKEN)")
	}
	if strings.TrimSpace(opts.RemoteProject) == "" || strings.TrimSpace(opts.RemoteEnv) == "" {
		return nil, fmt.Errorf("Doppler project and config are required")
	}
	base, err := normalizeBase(opts.BaseURL, "https://api.doppler.com")
	if err != nil {
		return nil, err
	}
	query := url.Values{}
	query.Set("project", opts.RemoteProject)
	query.Set("config", opts.RemoteEnv)
	query.Set("format", "json")
	endpoint := base + "/v3/configs/config/secrets/download?" + query.Encode()
	var values map[string]string
	if err := getJSON(ctx, opts, endpoint, opts.Token, &values); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(values))
	for key, value := range values {
		entries = append(entries, Entry{Key: key, Value: value, Kind: "secret", Source: "doppler"})
	}
	return entries, nil
}
