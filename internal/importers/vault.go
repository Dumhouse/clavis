package importers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func loadVault(ctx context.Context, opts Options) ([]Entry, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, fmt.Errorf("a Vault token is required (--token, VAULT_TOKEN, or OPENBAO_TOKEN)")
	}
	base, err := normalizeBase(opts.BaseURL, "http://127.0.0.1:8200")
	if err != nil {
		return nil, err
	}
	mount := strings.Trim(strings.TrimSpace(opts.VaultMount), "/")
	if mount == "" {
		mount = "secret"
	}
	path := strings.Trim(strings.TrimSpace(opts.RemotePath), "/")
	version := opts.VaultVersion
	if version == 0 {
		version = 2
	}
	endpoint := base + "/v1/" + mount
	if version == 2 {
		endpoint += "/data"
	}
	if path != "" {
		endpoint += "/" + path
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("X-Vault-Token", opts.Token)
	request.Header.Set("Accept", "application/json")
	response, err := httpClient(opts).Do(request)
	if err != nil {
		return nil, fmt.Errorf("request Vault: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("vault %s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode Vault response: %w", err)
	}
	values := payload.Data
	if version == 2 {
		nested, ok := payload.Data["data"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("the Vault response has no data.data map; check the KV mount version and path")
		}
		values = nested
	}
	entries := make([]Entry, 0, len(values))
	for key, raw := range values {
		value, ok := raw.(string)
		if !ok {
			encoded, _ := json.Marshal(raw)
			value = string(encoded)
		}
		entries = append(entries, Entry{Key: key, Value: value, Kind: "secret", Source: "vault"})
	}
	return entries, nil
}
