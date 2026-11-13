package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type syncJSON struct {
	ID           string     `json:"id"`
	Environment  string     `json:"environment"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status"`
	Confirmed    bool       `json:"confirmed"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitempty"`
}

type syncsResponse struct {
	Syncs []syncJSON `json:"syncs"`
}

type syncRunJSON struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`
	DryRun      bool       `json:"dry_run"`
	SecretCount int        `json:"secret_count"`
	Error       string     `json:"error,omitempty"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type syncRunsResponse struct {
	Runs []syncRunJSON `json:"runs"`
}

type runSyncResponse struct {
	Run     syncRunJSON `json:"run"`
	Preview *struct {
		Keys  []string `json:"keys"`
		Count int      `json:"count"`
	} `json:"preview,omitempty"`
}

func newSyncsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "syncs", Short: "Push secrets to external systems"}
	cmd.AddCommand(newSyncsListCommand(opts))
	cmd.AddCommand(newSyncsCreateCommand(opts))
	cmd.AddCommand(newSyncsRunCommand(opts))
	cmd.AddCommand(newSyncsLogsCommand(opts))
	cmd.AddCommand(newSyncsDriftCommand(opts))
	cmd.AddCommand(newSyncsBehaviorCommand(opts))
	return cmd
}

func syncsBase(resolved *resolvedContext) string {
	return "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/projects/" + url.PathEscape(resolved.Project) + "/syncs"
}

func (o *Options) syncContext() (*resolvedContext, error) {
	resolved, err := o.resolveContext(false)
	if err != nil {
		return nil, err
	}
	if resolved.Org == "" || resolved.Project == "" {
		return nil, fmt.Errorf("no project context: pass --org and --project or run `clavis init`")
	}
	return resolved, nil
}

func newSyncsListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List syncs for the linked project",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result syncsResponse
			if err := c.Get(cmd.Context(), syncsBase(resolved), &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Syncs)
			}
			rows := make([][]string, 0, len(result.Syncs))
			for _, sync := range result.Syncs {
				rows = append(rows, []string{sync.ID, sync.Environment, sync.Provider, sync.Status, fmt.Sprintf("%t", sync.Confirmed)})
			}
			out.Table([]string{"ID", "ENV", "PROVIDER", "STATUS", "CONFIRMED"}, rows)
			return nil
		},
	}
}

func newSyncsCreateCommand(opts *Options) *cobra.Command {
	var endpoint, secret, provider, baseURL, gitlabProject, token string
	var githubRepo, githubEnvironment, githubScope, vercelProject, vercelTeamID string
	var githubAppID, githubInstallation, githubPrivateKey string
	var awsRegion, awsAccessKey, awsSecretKey, awsSessionToken, awsService, awsPrefix string
	var gcpProject, gcpServiceAccount, gcpPrefix string
	var netlifyAccount, netlifySite, cfAccount, cfProject, cfScript, cfMode string
	var vaultMount, vaultPath string
	var forgejoURL, forgejoRepo, forgejoScope string
	var vaultVersion int
	var protected, masked, deleteMissing bool
	cmd := &cobra.Command{
		Use:   "create ENVIRONMENT",
		Short: "Create a sync to GitHub, GitLab, Vercel, AWS, an HTTP endpoint, and more",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if strings.HasPrefix(githubPrivateKey, "@") {
				data, err := os.ReadFile(strings.TrimPrefix(githubPrivateKey, "@"))
				if err != nil {
					return err
				}
				githubPrivateKey = string(data)
			}
			if strings.HasPrefix(gcpServiceAccount, "@") {
				data, err := os.ReadFile(strings.TrimPrefix(gcpServiceAccount, "@"))
				if err != nil {
					return err
				}
				gcpServiceAccount = string(data)
			}
			// --gitlab-url has a gitlab.com default; only send it for GitLab
			// (or when set explicitly) so other providers use their own default.
			if provider != "gitlab" && !cmd.Flags().Changed("gitlab-url") {
				baseURL = ""
			}
			if provider == "forgejo" {
				baseURL = forgejoURL
			}
			var created syncJSON
			body := map[string]any{
				"environment": args[0], "provider": provider,
				"endpoint": endpoint, "secret": secret,
				"base_url": baseURL, "gitlab_project": gitlabProject, "token": token,
				"protected": protected, "masked": masked,
				"github_repo": githubRepo, "github_environment": githubEnvironment,
				"github_scope":   githubScope,
				"vercel_project": vercelProject, "vercel_team_id": vercelTeamID,
				"aws_region": awsRegion, "aws_access_key": awsAccessKey,
				"aws_secret_key": awsSecretKey, "aws_session_token": awsSessionToken,
				"aws_service": awsService, "aws_prefix": awsPrefix,
				"gcp_project": gcpProject, "gcp_service_account": gcpServiceAccount,
				"gcp_prefix":      gcpPrefix,
				"netlify_account": netlifyAccount, "netlify_site": netlifySite,
				"cloudflare_account": cfAccount, "cloudflare_project": cfProject,
				"cloudflare_script": cfScript, "cloudflare_mode": cfMode,
				"vault_mount": vaultMount, "vault_path": vaultPath, "vault_version": vaultVersion,
				"forgejo_repo": forgejoRepo, "forgejo_scope": forgejoScope,
				"delete_missing": deleteMissing,
			}
			if err := c.Post(cmd.Context(), syncsBase(resolved), body, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Created sync %s for %s.", created.ID, created.Environment)
			out.Info("Preview the first run: clavis syncs run %s --dry-run", created.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "receiver URL for generic HTTP")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC signing secret for generic HTTP")
	cmd.Flags().StringVar(&provider, "provider", "http", "sync provider: http, gitlab, github, forgejo, vercel, aws, gcp, netlify, cloudflare, or vault")
	cmd.Flags().StringVar(&baseURL, "gitlab-url", "https://gitlab.com", "GitLab base URL")
	cmd.Flags().StringVar(&gitlabProject, "gitlab-project", "", "GitLab project path or ID")
	cmd.Flags().StringVar(&token, "gitlab-token", "", "GitLab access token with api scope")
	cmd.Flags().BoolVar(&protected, "protected", false, "mark GitLab variables protected")
	cmd.Flags().BoolVar(&masked, "masked", true, "mask GitLab variables when possible")
	cmd.Flags().StringVar(&githubRepo, "github-repo", "", "GitHub repository (owner/name) or org for org scope")
	cmd.Flags().StringVar(&githubEnvironment, "github-environment", "", "GitHub environment name")
	cmd.Flags().StringVar(&githubScope, "github-scope", "repo", "GitHub scope: repo, environment, or org")
	cmd.Flags().StringVar(&token, "github-token", "", "GitHub token with Actions secrets/variables write access")
	cmd.Flags().StringVar(&githubAppID, "github-app-id", "", "GitHub App ID")
	cmd.Flags().StringVar(&githubInstallation, "github-installation-id", "", "GitHub App installation ID")
	cmd.Flags().StringVar(&githubPrivateKey, "github-private-key", "", "GitHub App private key PEM or @file")
	cmd.Flags().StringVar(&forgejoURL, "forgejo-url", "https://codeberg.org", "Forgejo or Gitea base URL")
	cmd.Flags().StringVar(&forgejoRepo, "forgejo-repo", "", "Forgejo repository (owner/name) or organization for org scope")
	cmd.Flags().StringVar(&forgejoScope, "forgejo-scope", "repo", "Forgejo scope: repo or org")
	cmd.Flags().StringVar(&token, "forgejo-token", "", "Forgejo access token with write:repository (or write:organization)")
	cmd.Flags().StringVar(&token, "vercel-token", "", "Vercel access token")
	cmd.Flags().StringVar(&vercelProject, "vercel-project", "", "Vercel project ID or name")
	cmd.Flags().StringVar(&vercelTeamID, "vercel-team", "", "Vercel team ID")
	cmd.Flags().StringVar(&awsRegion, "aws-region", "", "AWS region")
	cmd.Flags().StringVar(&awsAccessKey, "aws-access-key", "", "AWS access key ID")
	cmd.Flags().StringVar(&awsSecretKey, "aws-secret-key", "", "AWS secret access key")
	cmd.Flags().StringVar(&awsSessionToken, "aws-session-token", "", "AWS session token")
	cmd.Flags().StringVar(&awsService, "aws-service", "ssm", "AWS service: ssm or secretsmanager")
	cmd.Flags().StringVar(&awsPrefix, "aws-prefix", "", "AWS parameter/secret name prefix")
	cmd.Flags().StringVar(&gcpProject, "gcp-project", "", "GCP project ID")
	cmd.Flags().StringVar(&gcpServiceAccount, "gcp-service-account-json", "", "GCP service account JSON or @file")
	cmd.Flags().StringVar(&gcpPrefix, "gcp-prefix", "", "GCP secret name prefix")
	cmd.Flags().StringVar(&netlifyAccount, "netlify-account", "", "Netlify account slug")
	cmd.Flags().StringVar(&netlifySite, "netlify-site", "", "Netlify site ID")
	cmd.Flags().StringVar(&token, "netlify-token", "", "Netlify personal access token")
	cmd.Flags().StringVar(&cfAccount, "cloudflare-account", "", "Cloudflare account ID")
	cmd.Flags().StringVar(&cfMode, "cloudflare-mode", "workers", "Cloudflare mode: workers or pages")
	cmd.Flags().StringVar(&vaultMount, "vault-mount", "secret", "Vault KV mount")
	cmd.Flags().StringVar(&vaultPath, "vault-path", "", "Vault path prefix (one secret per key)")
	cmd.Flags().IntVar(&vaultVersion, "vault-version", 2, "Vault KV version: 1 or 2")
	cmd.Flags().StringVar(&baseURL, "vault-url", "", "Vault/OpenBao address")
	cmd.Flags().StringVar(&token, "vault-token", "", "Vault/OpenBao token")
	cmd.Flags().StringVar(&cfProject, "cloudflare-project", "", "Cloudflare Pages project")
	cmd.Flags().StringVar(&cfScript, "cloudflare-script", "", "Cloudflare Workers script name")
	cmd.Flags().BoolVar(&deleteMissing, "delete-missing", false, "delete remote keys not present in Clavis")
	cmd.Flags().StringVar(&token, "cloudflare-token", "", "Cloudflare API token")
	return cmd
}

func newSyncsRunCommand(opts *Options) *cobra.Command {
	var dryRun, yes bool
	cmd := &cobra.Command{
		Use:   "run ID",
		Short: "Preview or execute a sync",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			body := map[string]bool{"dry_run": dryRun || !yes, "confirm": yes && !dryRun}
			var result runSyncResponse
			path := syncsBase(resolved) + "/" + url.PathEscape(args[0]) + "/run"
			if err := c.Post(cmd.Context(), path, body, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			if result.Preview != nil {
				out.Info("Preview: %d keys → %s", result.Preview.Count, strings.Join(result.Preview.Keys, ", "))
			}
			if result.Run.Status != "" {
				out.Success("Sync %s: %s (%d keys)", result.Run.ID, result.Run.Status, result.Run.SecretCount)
			}
			if result.Run.Error != "" {
				out.Errorf("%s", result.Run.Error)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview without sending values")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm and execute the sync")
	return cmd
}

func newSyncsLogsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "logs ID",
		Short: "Show recent sync runs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result syncRunsResponse
			path := syncsBase(resolved) + "/" + url.PathEscape(args[0]) + "/runs"
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Runs)
			}
			rows := make([][]string, 0, len(result.Runs))
			for _, run := range result.Runs {
				rows = append(rows, []string{run.StartedAt.Format(time.RFC3339), run.Status, fmt.Sprintf("%d", run.SecretCount), run.Error})
			}
			out.Table([]string{"STARTED", "STATUS", "KEYS", "ERROR"}, rows)
			return nil
		},
	}
}

func newSyncsDriftCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "drift ID",
		Short: "Check provider drift for a sync",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result struct {
				Supported bool     `json:"supported"`
				Keys      []string `json:"keys"`
				Detail    string   `json:"detail"`
			}
			path := syncsBase(resolved) + "/" + url.PathEscape(args[0]) + "/drift"
			if err := c.Post(cmd.Context(), path, map[string]string{}, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			if !result.Supported {
				out.Info("%s", result.Detail)
				return nil
			}
			if len(result.Keys) == 0 {
				out.Success("No drift detected.")
				return nil
			}
			out.Errorf("Drift in %d key(s): %s", len(result.Keys), strings.Join(result.Keys, ", "))
			return &ExitError{Code: ExitGeneral, Err: fmt.Errorf("drift detected"), Silent: true}
		},
	}
}

func newSyncsBehaviorCommand(opts *Options) *cobra.Command {
	var deleteMissing bool
	cmd := &cobra.Command{
		Use:   "behavior ID",
		Short: "Update sync behavior toggles",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.syncContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var updated syncJSON
			path := syncsBase(resolved) + "/" + url.PathEscape(args[0])
			if err := c.Patch(cmd.Context(), path, map[string]bool{"delete_missing": deleteMissing}, &updated); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(updated)
			}
			out.Success("Updated sync %s (delete_missing=%t).", args[0], deleteMissing)
			return nil
		},
	}
	cmd.Flags().BoolVar(&deleteMissing, "delete-missing", false, "delete remote keys not present in Clavis")
	return cmd
}
