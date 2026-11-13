package cli

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/pkg/problem"
)

type connectionJSON struct {
	ID         string     `json:"id"`
	Provider   string     `json:"provider"`
	Name       string     `json:"name"`
	BaseURL    string     `json:"base_url"`
	Auth       string     `json:"auth"`
	SyncCount  int        `json:"sync_count"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
}

func newConnectionsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connections",
		Short: "Manage org-wide provider credentials that syncs share",
		Long: "A connection stores one provider credential (a Forgejo or GitHub token, AWS keys, ...)\n" +
			"for the whole organization. Syncs created with --connection reuse it, and rotating\n" +
			"the connection updates every sync that uses it.",
	}
	cmd.AddCommand(newConnectionsListCommand(opts))
	cmd.AddCommand(newConnectionsCreateCommand(opts))
	cmd.AddCommand(newConnectionsRotateCommand(opts))
	cmd.AddCommand(newConnectionsTestCommand(opts))
	cmd.AddCommand(newConnectionsDeleteCommand(opts))
	return cmd
}

func (o *Options) connectionsBase() (*resolvedContext, string, error) {
	resolved, err := o.resolveContext(false)
	if err != nil {
		return nil, "", err
	}
	if resolved.Org == "" {
		return nil, "", fmt.Errorf("no organization: pass --org or run `clavis init`")
	}
	return resolved, "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/connections", nil
}

func newConnectionsListCommand(opts *Options) *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List connections (credentials are never shown)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, base, err := opts.connectionsBase()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if provider != "" {
				base += "?provider=" + url.QueryEscape(provider)
			}
			var result struct {
				Connections []connectionJSON `json:"connections"`
			}
			if err := c.Get(cmd.Context(), base, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Connections)
			}
			rows := make([][]string, 0, len(result.Connections))
			for _, conn := range result.Connections {
				used := "never"
				if conn.LastUsedAt != nil {
					used = conn.LastUsedAt.Local().Format("2006-01-02 15:04")
				}
				rows = append(rows, []string{conn.ID, conn.Name, conn.Provider, conn.BaseURL,
					fmt.Sprintf("%d", conn.SyncCount), used})
			}
			out.Table([]string{"ID", "NAME", "PROVIDER", "URL", "SYNCS", "LAST USED"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "only this provider")
	return cmd
}

// connectionCredentialFlags registers the credential flags shared by create
// and rotate.
func connectionCredentialFlags(cmd *cobra.Command, body map[string]*string) {
	flags := []struct{ name, key, usage string }{
		{"token", "token", "access token (Forgejo, GitHub, GitLab, Vercel, Netlify, Cloudflare, Vault); - reads stdin"},
		{"github-app-id", "github_app_id", "GitHub App ID"},
		{"github-installation-id", "github_installation_id", "GitHub App installation ID"},
		{"github-private-key", "github_private_key", "GitHub App private key PEM or @file"},
		{"aws-access-key", "aws_access_key", "AWS access key ID"},
		{"aws-secret-key", "aws_secret_key", "AWS secret access key"},
		{"aws-session-token", "aws_session_token", "AWS session token"},
		{"gcp-service-account-json", "gcp_service_account", "GCP service account JSON or @file"},
	}
	for _, flag := range flags {
		value := new(string)
		body[flag.key] = value
		cmd.Flags().StringVar(value, flag.name, "", flag.usage)
	}
}

func connectionRequestBody(fields map[string]*string) (map[string]string, error) {
	body := map[string]string{}
	for key, value := range fields {
		v := *value
		switch {
		case key == "token" && v == "-":
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return nil, err
			}
			v = strings.TrimSpace(string(data))
		case (key == "github_private_key" || key == "gcp_service_account") && strings.HasPrefix(v, "@"):
			data, err := os.ReadFile(strings.TrimPrefix(v, "@"))
			if err != nil {
				return nil, err
			}
			v = string(data)
		}
		if v != "" {
			body[key] = v
		}
	}
	return body, nil
}

func newConnectionsCreateCommand(opts *Options) *cobra.Command {
	var provider, baseURL string
	fields := map[string]*string{}
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Save a provider credential for the organization",
		Example: "  clavis connections create \"Home Forgejo\" --provider forgejo --url https://git.example.com --token -\n" +
			"  clavis syncs create dev --provider forgejo --forgejo-repo team/web --connection <ID>",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, base, err := opts.connectionsBase()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			body, err := connectionRequestBody(fields)
			if err != nil {
				return err
			}
			body["provider"] = provider
			body["name"] = args[0]
			if baseURL != "" {
				body["base_url"] = baseURL
			}
			var created connectionJSON
			if err := c.Post(cmd.Context(), base, body, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Saved connection %q (%s).", created.Name, created.ID)
			out.Info("Check it: clavis connections test %s", created.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "forgejo, github, gitlab, vercel, netlify, cloudflare, vault, aws, or gcp")
	cmd.Flags().StringVar(&baseURL, "url", "", "instance or API URL (defaults to the public service)")
	connectionCredentialFlags(cmd, fields)
	_ = cmd.MarkFlagRequired("provider")
	return cmd
}

func newConnectionsRotateCommand(opts *Options) *cobra.Command {
	var name, baseURL string
	fields := map[string]*string{}
	cmd := &cobra.Command{
		Use:   "rotate ID",
		Short: "Replace a connection's credential (or rename it / change its URL)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, base, err := opts.connectionsBase()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			body, err := connectionRequestBody(fields)
			if err != nil {
				return err
			}
			if name != "" {
				body["name"] = name
			}
			if baseURL != "" {
				body["base_url"] = baseURL
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to change: pass a credential flag, --name, or --url")
			}
			var updated connectionJSON
			if err := c.Patch(cmd.Context(), base+"/"+url.PathEscape(args[0]), body, &updated); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(updated)
			}
			out.Success("Updated connection %q; %d sync(s) use it.", updated.Name, updated.SyncCount)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "new name")
	cmd.Flags().StringVar(&baseURL, "url", "", "new instance or API URL")
	connectionCredentialFlags(cmd, fields)
	return cmd
}

func newConnectionsTestCommand(opts *Options) *cobra.Command {
	var repo, org string
	cmd := &cobra.Command{
		Use:   "test ID",
		Short: "Check that a connection works for the syncs that use it",
		Long: "Checks that the provider accepts the credential. For Forgejo it also checks " +
			"Actions access in every repository or organization the connection's syncs push to; " +
			"--repo or --org adds one more destination to check.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			if repo != "" && org != "" {
				return fmt.Errorf("use --repo or --org, not both")
			}
			resolved, base, err := opts.connectionsBase()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var body any
			switch {
			case repo != "":
				body = map[string]string{"forgejo_scope": "repo", "forgejo_repo": repo}
			case org != "":
				body = map[string]string{"forgejo_scope": "org", "forgejo_repo": org}
			}
			var result struct {
				OK       bool   `json:"ok"`
				Detail   string `json:"detail"`
				Identity string `json:"identity,omitempty"`
				Checks   []struct {
					Target string `json:"target"`
					OK     bool   `json:"ok"`
					Detail string `json:"detail"`
				} `json:"checks"`
			}
			if err := c.Post(cmd.Context(), base+"/"+url.PathEscape(args[0])+"/test", body, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			for _, check := range result.Checks {
				mark := "ok  "
				if !check.OK {
					mark = "FAIL"
				}
				out.Info("%s %s: %s", mark, check.Target, check.Detail)
			}
			if !result.OK {
				return fmt.Errorf("connection test failed: %s", result.Detail)
			}
			out.Success("%s", result.Detail)
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "Forgejo repository (owner/name) to check as well")
	cmd.Flags().StringVar(&org, "org", "", "Forgejo organization to check as well")
	return cmd
}

func newConnectionsDeleteCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rm ID",
		Short: "Delete a connection that no sync uses",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, base, err := opts.connectionsBase()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := c.Delete(cmd.Context(), base+"/"+url.PathEscape(args[0])); err != nil {
				var p *problem.Problem
				if errors.As(err, &p) && p.Code == "connection_in_use" {
					lines := make([]string, 0, len(p.Errors))
					for _, entry := range p.Errors {
						lines = append(lines, "  "+strings.TrimPrefix(entry.Field, "syncs.")+"  "+entry.Detail)
					}
					return fmt.Errorf("connection is used by %d sync(s); delete them or move them with `clavis syncs use-connection` first:\n%s",
						len(p.Errors), strings.Join(lines, "\n"))
				}
				return err
			}
			out.Success("Deleted connection %s.", args[0])
			return nil
		},
	}
}
