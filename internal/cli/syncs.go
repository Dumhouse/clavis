package cli

import (
	"fmt"
	"net/url"
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
	var endpoint, secret, provider string
	cmd := &cobra.Command{
		Use:   "create ENVIRONMENT",
		Short: "Create a generic HTTP sync",
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
			var created syncJSON
			body := map[string]string{
				"environment": args[0], "provider": provider,
				"endpoint": endpoint, "secret": secret,
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
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "receiver URL (required)")
	cmd.Flags().StringVar(&secret, "secret", "", "HMAC signing secret")
	cmd.Flags().StringVar(&provider, "provider", "http", "sync provider")
	_ = cmd.MarkFlagRequired("endpoint")
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
