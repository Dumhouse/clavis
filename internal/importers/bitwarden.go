package importers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// bwsSecret is one item from the JSON that `bws secret list` prints.
type bwsSecret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// loadBitwarden shells out to the Bitwarden Secrets Manager CLI (bws) because
// it owns the login and the access token the user already configured.
func loadBitwarden(ctx context.Context, opts Options) ([]Entry, error) {
	project := strings.TrimSpace(opts.RemoteProject)
	if project == "" {
		return nil, fmt.Errorf("a Bitwarden Secrets Manager project ID is required (--remote-project)")
	}
	binary, err := exec.LookPath("bws")
	if err != nil {
		return nil, fmt.Errorf("find the Bitwarden Secrets Manager CLI (bws) on PATH: %w", err)
	}
	//nolint:gosec // G204: bws is resolved from PATH and the project ID is operator input
	command := exec.CommandContext(ctx, binary, "secret", "list", project, "--output", "json")
	command.Env = bitwardenEnv(opts.Token)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if message := strings.TrimSpace(stderr.String()); message != "" {
			return nil, fmt.Errorf("bws secret list: %w: %s", err, message)
		}
		return nil, fmt.Errorf("bws secret list: %w", err)
	}
	return parseBitwardenSecrets(output)
}

// bitwardenEnv returns the process environment with the access token from the
// flag, replacing any inherited BWS_ACCESS_TOKEN so --token wins.
func bitwardenEnv(token string) []string {
	env := os.Environ()
	if strings.TrimSpace(token) == "" {
		return env
	}
	filtered := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.EqualFold(name, "BWS_ACCESS_TOKEN") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return append(filtered, "BWS_ACCESS_TOKEN="+token)
}

func parseBitwardenSecrets(raw []byte) ([]Entry, error) {
	var secrets []bwsSecret
	if err := json.Unmarshal(raw, &secrets); err != nil {
		return nil, fmt.Errorf("decode bws output: %w", err)
	}
	entries := make([]Entry, 0, len(secrets))
	for _, secret := range secrets {
		key := strings.TrimSpace(secret.Key)
		if key == "" {
			continue
		}
		entries = append(entries, Entry{Key: key, Value: secret.Value, Kind: "secret", Source: "bitwarden"})
	}
	return entries, nil
}
