package importers

import (
	"context"
	"fmt"
	"strings"
)

func loadOnePassword(ctx context.Context, opts Options) ([]Entry, error) {
	if strings.TrimSpace(opts.Token) == "" {
		return nil, fmt.Errorf("1Password Connect token is required (--token or OP_CONNECT_TOKEN)")
	}
	if strings.TrimSpace(opts.OnePasswordVault) == "" {
		return nil, fmt.Errorf("1Password vault ID is required (--op-vault or OP_VAULT)")
	}
	base, err := normalizeBase(opts.BaseURL, "")
	if err != nil {
		return nil, fmt.Errorf("1Password Connect host is required (--base-url or OP_CONNECT_HOST)")
	}
	var list struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := getJSON(ctx, opts, base+"/v1/vaults/"+opts.OnePasswordVault+"/items", opts.Token, &list); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(list.Items))
	for _, item := range list.Items {
		var detail struct {
			Title  string `json:"title"`
			Fields []struct {
				Label string `json:"label"`
				Value string `json:"value"`
				Type  string `json:"type"`
			} `json:"fields"`
		}
		endpoint := base + "/v1/vaults/" + opts.OnePasswordVault + "/items/" + item.ID
		if err := getJSON(ctx, opts, endpoint, opts.Token, &detail); err != nil {
			return nil, err
		}
		for _, field := range detail.Fields {
			if field.Value == "" || field.Type == "OTP" {
				continue
			}
			key := strings.TrimSpace(field.Label)
			if key == "" {
				key = detail.Title
			}
			if key == "" {
				continue
			}
			entries = append(entries, Entry{
				Key: keyToEnv(key), Value: field.Value, Kind: "secret", Source: "1password",
			})
		}
	}
	return entries, nil
}

func keyToEnv(key string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.TrimSpace(key) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
			lastUnderscore = false
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "VALUE"
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "VALUE_" + out
	}
	return out
}
