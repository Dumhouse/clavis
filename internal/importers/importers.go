// Package importers fetches secrets from external systems into a common shape
// for Clavis' import preview and apply flow.
package importers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Entry is one imported key/value pair.
type Entry struct {
	Key    string
	Value  string
	Kind   string
	Source string
}

// Options configures a provider fetch.
type Options struct {
	Provider string
	BaseURL  string
	Token    string

	RemoteProject string
	RemoteEnv     string
	RemotePath    string

	VaultMount   string
	VaultVersion int

	OnePasswordVault string

	AWSRegion       string
	AWSAccessKey    string
	AWSSecretKey    string
	AWSSessionToken string
	AWSPrefix       string
	AWSKind         string

	GCPProject        string
	GCPServiceAccount string
	GCPPrefix         string

	HTTPClient *http.Client
}

// Load fetches entries from the selected provider. File and directory imports
// stay in the CLI so they can reuse the existing dotenv parser.
func Load(ctx context.Context, opts Options) ([]Entry, error) {
	var entries []Entry
	var err error
	switch strings.ToLower(strings.TrimSpace(opts.Provider)) {
	case "infisical":
		entries, err = loadInfisical(ctx, opts)
	case "doppler":
		entries, err = loadDoppler(ctx, opts)
	case "vault", "openbao":
		entries, err = loadVault(ctx, opts)
	case "onepassword", "1password":
		entries, err = loadOnePassword(ctx, opts)
	case "aws", "aws-secrets-manager":
		entries, err = loadAWS(ctx, opts)
	case "gcp", "gcp-secret-manager":
		entries, err = loadGCP(ctx, opts)
	default:
		return nil, fmt.Errorf("unsupported import provider %q", opts.Provider)
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

func httpClient(opts Options) *http.Client {
	if opts.HTTPClient != nil {
		return opts.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func getJSON(ctx context.Context, opts Options, endpoint, token string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := httpClient(opts).Do(request)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s returned %d: %s", endpoint, response.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("decode %s: %w", endpoint, err)
	}
	return nil
}

func normalizeBase(raw, fallback string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = fallback
	}
	parsed, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("base URL must be an absolute http(s) URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("base URL must use http or https")
	}
	return parsed.String(), nil
}

func keyValue(value string) (map[string]string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "{") {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, false
	}
	result := make(map[string]string, len(out))
	for key, raw := range out {
		switch typed := raw.(type) {
		case string:
			result[key] = typed
		case bool:
			result[key] = fmt.Sprintf("%t", typed)
		case float64:
			result[key] = fmt.Sprintf("%v", typed)
		case nil:
			result[key] = ""
		default:
			encoded, _ := json.Marshal(typed)
			result[key] = string(encoded)
		}
	}
	return result, true
}
