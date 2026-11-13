package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/dumhouse/clavis/internal/store"
)

// maxForgejoTestTargets caps how many destinations one test probes.
const maxForgejoTestTargets = 5

// testForgejoConnection checks what a sync actually needs: that the server is
// reachable, that the token is valid, and that it can manage Actions secrets
// in every repository or organization its syncs push to. GET /api/v1/user is
// not enough on its own: it needs the token's "user" permission, which a
// sync-only token does not have, so a 403 there still means a valid token.
func (s *Service) testForgejoConnection(ctx context.Context, org *store.Org, conn *store.SyncConnection, config syncConfig, extra *ConnectionTestTarget) (*ConnectionTest, error) {
	client := s.forgejoClient()
	base := strings.TrimSuffix(strings.TrimRight(config.BaseURL, "/"), "/api/v1")
	token, err := s.decryptOrgSecret(org, conn.ID, "sync_token", config.Token)
	if err != nil {
		return nil, err
	}

	result := &ConnectionTest{}
	status, body, err := forgejoRequest(ctx, client, http.MethodGet, base+"/api/v1/user", token, nil)
	tokenCheck := ConnectionCheck{Target: "Token"}
	switch {
	case err != nil:
		tokenCheck.Detail = fmt.Sprintf("Cannot reach %s: %v. Check the Forgejo URL (the instance root, without /api/v1), DNS, TLS, and that the Clavis server can reach it.", base, err)
	case status == http.StatusUnauthorized:
		tokenCheck.Detail = "Forgejo rejected the token: it is wrong, expired, or was deleted. Create a new one under Settings → Applications and rotate this connection."
	case status == http.StatusForbidden:
		// Valid token without the "user" permission: fine for syncs.
		tokenCheck.OK = true
		tokenCheck.Detail = "Token is valid (it has no \"user\" permission, which syncs do not need)."
	case status >= 200 && status < 300:
		var user struct {
			Login    string `json:"login"`
			Username string `json:"username"`
		}
		_ = json.Unmarshal(body, &user)
		result.Identity = user.Login
		if result.Identity == "" {
			result.Identity = user.Username
		}
		tokenCheck.OK = true
		tokenCheck.Detail = "Token is valid"
		if result.Identity != "" {
			tokenCheck.Detail += " for " + result.Identity
		}
		tokenCheck.Detail += "."
	case status == http.StatusNotFound:
		tokenCheck.Detail = fmt.Sprintf("%s/api/v1/user returned 404: this does not look like a Forgejo or Gitea server. Check the URL.", base)
	default:
		tokenCheck.Detail = fmt.Sprintf("Forgejo returned HTTP %d: %s", status, trimDetail(body))
	}
	result.Checks = append(result.Checks, tokenCheck)
	if !tokenCheck.OK {
		result.Detail = tokenCheck.Detail
		return result, nil
	}

	targets, err := s.forgejoTestTargets(ctx, org.ID, conn.ID, extra)
	if err != nil {
		return nil, err
	}
	for _, target := range targets {
		result.Checks = append(result.Checks, forgejoTargetCheck(ctx, client, base, token, target))
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
		switch len(targets) {
		case 0:
			result.Detail = tokenCheck.Detail + " No sync uses this connection yet, so repository access was not checked; enter a repository to check it."
		case 1:
			result.Detail = tokenCheck.Detail + " It can manage Actions secrets in " + targets[0].ForgejoRepo + "."
		default:
			result.Detail = fmt.Sprintf("%s It can manage Actions secrets in all %d destinations.", tokenCheck.Detail, len(targets))
		}
	}
	return result, nil
}

// forgejoTestTargets returns the user's extra target followed by the
// destinations of syncs that use the connection, deduplicated and capped.
func (s *Service) forgejoTestTargets(ctx context.Context, orgID, connectionID string, extra *ConnectionTestTarget) ([]ConnectionTestTarget, error) {
	var targets []ConnectionTestTarget
	seen := map[string]bool{}
	add := func(scope, repo string) {
		scope = strings.ToLower(strings.TrimSpace(scope))
		if scope == "" {
			scope = "repo"
		}
		repo = strings.Trim(strings.TrimSpace(repo), "/")
		key := scope + ":" + strings.ToLower(repo)
		if repo == "" || seen[key] || len(targets) >= maxForgejoTestTargets {
			return
		}
		seen[key] = true
		targets = append(targets, ConnectionTestTarget{ForgejoScope: scope, ForgejoRepo: repo})
	}
	if extra != nil {
		add(extra.ForgejoScope, extra.ForgejoRepo)
	}
	usage, err := s.Store.ListSyncConnectionUsage(ctx, orgID, connectionID)
	if err != nil {
		return nil, err
	}
	for _, item := range usage[connectionID] {
		sync, err := s.Store.GetSync(ctx, item.ProjectID, item.SyncID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				continue
			}
			return nil, err
		}
		var config syncConfig
		if err := json.Unmarshal([]byte(sync.Config), &config); err != nil {
			continue
		}
		add(config.ForgejoScope, config.ForgejoRepo)
	}
	return targets, nil
}

// forgejoTargetCheck lists Actions secrets, the same call a sync makes before
// writing, and explains each failure in terms of what to change.
func forgejoTargetCheck(ctx context.Context, client *http.Client, base, token string, target ConnectionTestTarget) ConnectionCheck {
	label := "Repository " + target.ForgejoRepo
	permission := "repository"
	if target.ForgejoScope == "org" {
		label = "Organization " + target.ForgejoRepo
		permission = "organization"
	}
	check := ConnectionCheck{Target: label}
	if target.ForgejoScope == "repo" {
		parts := strings.Split(target.ForgejoRepo, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			check.Detail = "Use owner/name, for example team/web."
			return check
		}
	} else if strings.Contains(target.ForgejoRepo, "/") {
		check.Detail = "An organization is just its name, without a slash."
		return check
	}
	config := syncConfig{BaseURL: base, ForgejoScope: target.ForgejoScope, ForgejoRepo: target.ForgejoRepo}
	status, body, err := forgejoRequest(ctx, client, http.MethodGet, forgejoActionsBase(config)+"/secrets", token, nil)
	switch {
	case err != nil:
		check.Detail = fmt.Sprintf("Request failed: %v.", err)
	case status >= 200 && status < 300:
		check.OK = true
		check.Detail = "Can manage Actions secrets."
	case status == http.StatusUnauthorized:
		check.Detail = "Forgejo rejected the token for this request."
	case status == http.StatusForbidden:
		if strings.Contains(strings.ToLower(string(body)), "scope") {
			check.Detail = fmt.Sprintf("The token is missing the %q permission. Create a token with %s set to Read and Write.", permission, permission)
		} else {
			check.Detail = fmt.Sprintf("The token's owner cannot manage Actions here. They need admin (or write) access to this %s.", permission)
		}
	case status == http.StatusNotFound:
		check.Detail = fmt.Sprintf("Not found. Check the spelling, that the token's owner can see this %s, and that Actions is enabled for it (Settings → Units/Actions).", permission)
	default:
		check.Detail = fmt.Sprintf("Forgejo returned HTTP %d: %s", status, trimDetail(body))
	}
	return check
}

func trimDetail(body []byte) string {
	detail := strings.TrimSpace(string(body))
	if len(detail) > 200 {
		detail = detail[:200]
	}
	if detail == "" {
		return "(empty response)"
	}
	return detail
}
