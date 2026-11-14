package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/dumhouse/clavis/internal/authz"
	"github.com/dumhouse/clavis/internal/store"
)

// RepositoryListInput names a provider credential: either a saved connection
// or a URL and token typed into the sync form (never stored by this call).
type RepositoryListInput struct {
	Provider     string
	BaseURL      string
	Token        string
	ConnectionID string
}

// Repository is one place a sync can push to.
type Repository struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
	// CanWrite is false when the token can see the repository but probably
	// cannot write its secrets (no admin or maintainer rights).
	CanWrite bool   `json:"can_write"`
	URL      string `json:"url,omitempty"`
}

// RepositoryList is what the token can reach, for the sync form's picker.
type RepositoryList struct {
	Repositories  []Repository `json:"repositories"`
	Organizations []string     `json:"organizations"`
	Identity      string       `json:"identity,omitempty"`
	// Truncated is set when there were more repositories than one listing
	// returns; the form then also accepts a typed name.
	Truncated bool `json:"truncated"`
}

const repositoryListLimit = 500

// ListRepositories lists the repositories and organizations a credential can
// reach on Forgejo/Gitea, GitHub or GitLab, so the sync form can offer them
// instead of asking the user to type owner/name.
func (s *Service) ListRepositories(ctx context.Context, actor authz.Actor, orgSlug, projectSlug string, in RepositoryListInput) (*RepositoryList, error) {
	org, _, err := s.projectForWrite(ctx, actor, orgSlug, projectSlug, authz.SyncsManage)
	if err != nil {
		return nil, err
	}
	provider := strings.TrimSpace(in.Provider)
	base, token := strings.TrimSpace(in.BaseURL), strings.TrimSpace(in.Token)
	if in.ConnectionID != "" {
		conn, err := s.Store.GetSyncConnection(ctx, org.ID, in.ConnectionID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("%w: platform token not found", ErrValidation)
			}
			return nil, err
		}
		if provider != "" && provider != conn.Provider {
			return nil, fmt.Errorf("%w: that credential is for %s", ErrValidation, conn.Provider)
		}
		provider = conn.Provider
		var config syncConfig
		if err := json.Unmarshal([]byte(conn.Config), &config); err != nil {
			return nil, err
		}
		base = conn.BaseURL
		if provider == "github" && len(config.Token) == 0 {
			config.BaseURL = conn.BaseURL
			token, err = s.githubSyncToken(ctx, org, &store.Sync{ID: conn.ID, ConnectionID: conn.ID, Provider: provider}, config)
			if err != nil {
				return nil, err
			}
			return s.listGitHubAppRepositories(ctx, base, token)
		}
		if token, err = s.decryptOrgSecret(org, conn.ID, "sync_token", config.Token); err != nil {
			return nil, err
		}
	}
	if token == "" {
		return nil, fmt.Errorf("%w: enter an access token or pick a platform token first", ErrValidation)
	}
	base, err = normalizeConnectionBaseURL(provider, base)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	switch provider {
	case "forgejo":
		return s.listForgejoRepositories(ctx, base, token)
	case "github":
		return s.listGitHubRepositories(ctx, base, token)
	case "gitlab":
		return s.listGitLabRepositories(ctx, base, token)
	default:
		return nil, fmt.Errorf("%w: listing repositories works for Forgejo, GitHub and GitLab", ErrValidation)
	}
}

// repoFetchError turns an HTTP failure into advice for the person filling in
// the form.
func repoFetchError(provider, base string, status int, err error) error {
	switch {
	case err != nil:
		return fmt.Errorf("%w: could not reach %s: %w. Check the URL, and that the Clavis server can reach it (private addresses need CLAVIS_OUTBOUND_ALLOWLIST)", ErrValidation, base, err)
	case status == http.StatusUnauthorized:
		return fmt.Errorf("%w: %s rejected the token: it is wrong, expired or revoked", ErrValidation, provider)
	case status == http.StatusForbidden:
		return fmt.Errorf("%w: the token may not list repositories. Type the repository name instead, or give the token read access to repositories", ErrValidation)
	case status == http.StatusNotFound:
		return fmt.Errorf("%w: %s does not look like a %s server (HTTP 404). Use the instance root URL", ErrValidation, base, provider)
	default:
		return fmt.Errorf("%w: %s returned HTTP %d while listing repositories", ErrValidation, provider, status)
	}
}

func (s *Service) listForgejoRepositories(ctx context.Context, base, token string) (*RepositoryList, error) {
	client := s.forgejoClient()
	out := &RepositoryList{Repositories: []Repository{}, Organizations: []string{}}
	for page := 1; ; page++ {
		status, body, err := forgejoRequest(ctx, client, http.MethodGet,
			fmt.Sprintf("%s/api/v1/user/repos?limit=50&page=%d", base, page), token, nil)
		if err != nil || status != http.StatusOK {
			return nil, repoFetchError("Forgejo", base, status, err)
		}
		var repos []struct {
			FullName    string `json:"full_name"`
			Private     bool   `json:"private"`
			HTMLURL     string `json:"html_url"`
			Archived    bool   `json:"archived"`
			Permissions struct {
				Admin bool `json:"admin"`
				Push  bool `json:"push"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, fmt.Errorf("%w: unexpected answer from %s: %w", ErrValidation, base, err)
		}
		for _, repo := range repos {
			if repo.Archived {
				continue
			}
			out.Repositories = append(out.Repositories, Repository{
				FullName: repo.FullName, Private: repo.Private, URL: repo.HTMLURL,
				// Actions secrets need admin on the repository.
				CanWrite: repo.Permissions.Admin,
			})
		}
		if len(repos) < 50 {
			break
		}
		if len(out.Repositories) >= repositoryListLimit {
			out.Truncated = true
			break
		}
	}
	// Organizations are optional: a token without the organization scope
	// still lists repositories.
	if status, body, err := forgejoRequest(ctx, client, http.MethodGet, base+"/api/v1/user/orgs?limit=50", token, nil); err == nil && status == http.StatusOK {
		var orgs []struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		}
		if json.Unmarshal(body, &orgs) == nil {
			for _, o := range orgs {
				name := o.Username
				if name == "" {
					name = o.Name
				}
				out.Organizations = append(out.Organizations, name)
			}
		}
	}
	if status, body, err := forgejoRequest(ctx, client, http.MethodGet, base+"/api/v1/user", token, nil); err == nil && status == http.StatusOK {
		var user struct {
			Login string `json:"login"`
		}
		_ = json.Unmarshal(body, &user)
		out.Identity = user.Login
	}
	sortRepositories(out)
	return out, nil
}

func (s *Service) listGitHubRepositories(ctx context.Context, base, token string) (*RepositoryList, error) {
	if base == "" {
		base = "https://api.github.com"
	}
	client := s.forgejoClient()
	out := &RepositoryList{Repositories: []Repository{}, Organizations: []string{}}
	for page := 1; ; page++ {
		status, body, err := githubRequest(ctx, client, http.MethodGet,
			fmt.Sprintf("%s/user/repos?per_page=100&page=%d&sort=pushed", base, page), token, nil)
		if err != nil || status != http.StatusOK {
			return nil, repoFetchError("GitHub", base, status, err)
		}
		var repos []struct {
			FullName    string `json:"full_name"`
			Private     bool   `json:"private"`
			HTMLURL     string `json:"html_url"`
			Archived    bool   `json:"archived"`
			Permissions struct {
				Admin bool `json:"admin"`
			} `json:"permissions"`
		}
		if err := json.Unmarshal(body, &repos); err != nil {
			return nil, fmt.Errorf("%w: unexpected answer from GitHub: %v", ErrValidation, err)
		}
		for _, repo := range repos {
			if !repo.Archived {
				out.Repositories = append(out.Repositories, Repository{
					FullName: repo.FullName, Private: repo.Private, URL: repo.HTMLURL, CanWrite: repo.Permissions.Admin,
				})
			}
		}
		if len(repos) < 100 {
			break
		}
		if len(out.Repositories) >= repositoryListLimit {
			out.Truncated = true
			break
		}
	}
	if status, body, err := githubRequest(ctx, client, http.MethodGet, base+"/user/orgs?per_page=100", token, nil); err == nil && status == http.StatusOK {
		var orgs []struct {
			Login string `json:"login"`
		}
		if json.Unmarshal(body, &orgs) == nil {
			for _, o := range orgs {
				out.Organizations = append(out.Organizations, o.Login)
			}
		}
	}
	sortRepositories(out)
	return out, nil
}

func (s *Service) listGitHubAppRepositories(ctx context.Context, base, token string) (*RepositoryList, error) {
	if base == "" {
		base = "https://api.github.com"
	}
	status, body, err := githubRequest(ctx, s.forgejoClient(), http.MethodGet, base+"/installation/repositories?per_page=100", token, nil)
	if err != nil || status != http.StatusOK {
		return nil, repoFetchError("GitHub", base, status, err)
	}
	var page struct {
		TotalCount   int `json:"total_count"`
		Repositories []struct {
			FullName string `json:"full_name"`
			Private  bool   `json:"private"`
			HTMLURL  string `json:"html_url"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, err
	}
	out := &RepositoryList{Repositories: []Repository{}, Organizations: []string{}, Truncated: page.TotalCount > len(page.Repositories)}
	for _, repo := range page.Repositories {
		// The App's installation decides the permissions; assume it can write.
		out.Repositories = append(out.Repositories, Repository{FullName: repo.FullName, Private: repo.Private, URL: repo.HTMLURL, CanWrite: true})
	}
	sortRepositories(out)
	return out, nil
}

func (s *Service) listGitLabRepositories(ctx context.Context, base, token string) (*RepositoryList, error) {
	if base == "" {
		base = "https://gitlab.com"
	}
	client := s.forgejoClient()
	out := &RepositoryList{Repositories: []Repository{}, Organizations: []string{}}
	for page := 1; ; page++ {
		// min_access_level=40: Maintainer, which CI/CD variables need.
		endpoint := fmt.Sprintf("%s/api/v4/projects?membership=true&min_access_level=40&archived=false&per_page=100&page=%d&order_by=last_activity_at", base, page)
		status, body, err := headerRequest(ctx, client, endpoint, "PRIVATE-TOKEN", token)
		if err != nil || status != http.StatusOK {
			return nil, repoFetchError("GitLab", base, status, err)
		}
		var projects []struct {
			Path       string `json:"path_with_namespace"`
			Visibility string `json:"visibility"`
			WebURL     string `json:"web_url"`
		}
		if err := json.Unmarshal(body, &projects); err != nil {
			return nil, fmt.Errorf("%w: unexpected answer from GitLab: %v", ErrValidation, err)
		}
		for _, p := range projects {
			out.Repositories = append(out.Repositories, Repository{FullName: p.Path, Private: p.Visibility != "public", URL: p.WebURL, CanWrite: true})
		}
		if len(projects) < 100 {
			break
		}
		if len(out.Repositories) >= repositoryListLimit {
			out.Truncated = true
			break
		}
	}
	sortRepositories(out)
	return out, nil
}

func sortRepositories(list *RepositoryList) {
	sort.SliceStable(list.Repositories, func(i, j int) bool {
		a, b := list.Repositories[i], list.Repositories[j]
		if a.CanWrite != b.CanWrite {
			return a.CanWrite
		}
		return strings.ToLower(a.FullName) < strings.ToLower(b.FullName)
	})
	sort.Strings(list.Organizations)
}

// SyncTarget is a short, credential-free label of where a sync pushes, such
// as "team/web" or "acme/web · environment production", for lists and for
// spotting two syncs that write to the same place.
func SyncTarget(sync *store.Sync) string {
	var c syncConfig
	if err := json.Unmarshal([]byte(sync.Config), &c); err != nil {
		return ""
	}
	switch sync.Provider {
	case "forgejo":
		if c.ForgejoScope == "org" {
			return "organization " + c.ForgejoRepo
		}
		return c.ForgejoRepo
	case "github":
		switch c.GitHubScope {
		case "org":
			return "organization " + c.GitHubRepo
		case "environment":
			return c.GitHubRepo + " · environment " + c.GitHubEnv
		}
		return c.GitHubRepo
	case "gitlab":
		return c.Project
	case "vercel":
		return c.VercelProject
	case "netlify":
		return strings.Trim(c.NetlifyAccount+"/"+c.NetlifySite, "/")
	case "cloudflare":
		if c.CFMode == "pages" {
			return "Pages " + c.CFProject
		}
		return "Worker " + c.CFScript
	case "aws":
		return c.AWSService + " " + c.AWSRegion + " " + c.AWSPrefix
	case "gcp":
		return c.GCPProject
	case "vault":
		return strings.Trim(c.VaultMount+"/"+c.VaultPath, "/")
	case "http":
		return c.Endpoint
	}
	return ""
}
