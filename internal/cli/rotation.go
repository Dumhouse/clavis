package cli

import (
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type rotationPolicyJSON struct {
	ID          string     `json:"id"`
	Environment string     `json:"environment"`
	Key         string     `json:"key"`
	Provider    string     `json:"provider"`
	Interval    int        `json:"interval_seconds"`
	Enabled     bool       `json:"enabled"`
	NextRunAt   time.Time  `json:"next_run_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	LastStatus  string     `json:"last_status,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type rotationPoliciesResponse struct {
	Policies []rotationPolicyJSON `json:"policies"`
}

type rotationRunJSON struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"`
	Phase      string     `json:"phase"`
	Message    string     `json:"message,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type rotationRunsResponse struct {
	Runs []rotationRunJSON `json:"runs"`
}

func newRotationCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rotation",
		Short: "Schedule and run secret rotation",
	}
	cmd.AddCommand(newRotationListCommand(opts))
	cmd.AddCommand(newRotationStatusCommand(opts))
	cmd.AddCommand(newRotationCreateCommand(opts))
	cmd.AddCommand(newRotationRunCommand(opts))
	cmd.AddCommand(newRotationToggleCommand(opts, true))
	cmd.AddCommand(newRotationToggleCommand(opts, false))
	cmd.AddCommand(newRotationRunsCommand(opts))
	cmd.AddCommand(newRotationDeleteCommand(opts))
	return cmd
}

func rotationBase(resolved *resolvedContext) string {
	return fmt.Sprintf("/api/v1/orgs/%s/projects/%s/environments/%s/rotations",
		url.PathEscape(resolved.Org), url.PathEscape(resolved.Project), url.PathEscape(resolved.Env))
}

func newRotationListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List rotation policies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result rotationPoliciesResponse
			if err := c.Get(cmd.Context(), rotationBase(resolved), &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Policies)
			}
			rows := make([][]string, 0, len(result.Policies))
			for _, policy := range result.Policies {
				enabled := "no"
				if policy.Enabled {
					enabled = "yes"
				}
				rows = append(rows, []string{
					policy.ID, policy.Key, policy.Provider, fmt.Sprintf("%ds", policy.Interval),
					enabled, policy.NextRunAt.Local().Format(time.DateTime), policy.LastStatus,
				})
			}
			out.Table([]string{"ID", "KEY", "PROVIDER", "INTERVAL", "ENABLED", "NEXT RUN", "STATUS"}, rows)
			return nil
		},
	}
}

func newRotationStatusCommand(opts *Options) *cobra.Command {
	var staleDays int
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show managed and stale manual-rotation secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result struct {
				Environment string `json:"environment"`
				StaleDays   int    `json:"stale_days"`
				Managed     int    `json:"managed"`
				Unmanaged   int    `json:"unmanaged"`
				Stale       []struct {
					Key            string    `json:"key"`
					OwnerGroupName string    `json:"owner_group_name,omitempty"`
					UpdatedAt      time.Time `json:"updated_at"`
					AgeDays        int       `json:"age_days"`
				} `json:"stale"`
			}
			path := rotationBase(resolved) + "/status?stale_days=" + fmt.Sprintf("%d", staleDays)
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			out.Info("%s: %d managed, %d unmanaged, %d stale (>= %d days).", result.Environment, result.Managed, result.Unmanaged, len(result.Stale), result.StaleDays)
			rows := make([][]string, 0, len(result.Stale))
			for _, secret := range result.Stale {
				rows = append(rows, []string{secret.Key, fmt.Sprintf("%d", secret.AgeDays), secret.OwnerGroupName, secret.UpdatedAt.Local().Format("2006-01-02")})
			}
			out.Table([]string{"KEY", "AGE (DAYS)", "OWNER GROUP", "UPDATED"}, rows)
			return nil
		},
	}
	cmd.Flags().IntVar(&staleDays, "stale-days", 90, "warn after this many days without rotation")
	return cmd
}

func newRotationCreateCommand(opts *Options) *cobra.Command {
	var provider string
	var interval time.Duration
	var enabled bool
	var pgHost, pgDatabase, pgUser, pgPassword, pgSSL, pgRole string
	var pgPort int
	var awsRegion, awsAccessKey, awsSecretKey, awsUser string
	var webhookURL, webhookSecret string
	cmd := &cobra.Command{
		Use:   "create KEY",
		Short: "Create a rotation policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var config map[string]any
			switch provider {
			case "postgres":
				config = map[string]any{
					"host": pgHost, "port": pgPort, "database": pgDatabase,
					"username": pgUser, "password": pgPassword, "sslmode": pgSSL, "role": pgRole,
				}
			case "aws":
				config = map[string]any{
					"region": awsRegion, "access_key": awsAccessKey,
					"secret_key": awsSecretKey, "user_name": awsUser,
				}
			case "webhook":
				config = map[string]any{"url": webhookURL, "secret": webhookSecret}
			default:
				return fmt.Errorf("provider must be postgres, aws, or webhook")
			}
			var created rotationPolicyJSON
			if err := c.Post(cmd.Context(), rotationBase(resolved), map[string]any{
				"key": args[0], "provider": provider, "config": config,
				"interval_seconds": int(interval.Seconds()), "enabled": enabled,
			}, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Created rotation policy %s for %s every %s.", created.ID, created.Key, interval)
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "postgres", "postgres, aws, or webhook")
	cmd.Flags().DurationVar(&interval, "interval", 24*time.Hour, "rotation interval")
	cmd.Flags().BoolVar(&enabled, "enabled", true, "enable the schedule")
	cmd.Flags().StringVar(&pgHost, "pg-host", "localhost", "PostgreSQL host")
	cmd.Flags().IntVar(&pgPort, "pg-port", 5432, "PostgreSQL port")
	cmd.Flags().StringVar(&pgDatabase, "pg-database", "", "PostgreSQL database")
	cmd.Flags().StringVar(&pgUser, "pg-username", "", "PostgreSQL admin user")
	cmd.Flags().StringVar(&pgPassword, "pg-password", "", "PostgreSQL admin password")
	cmd.Flags().StringVar(&pgSSL, "pg-sslmode", "disable", "PostgreSQL sslmode")
	cmd.Flags().StringVar(&pgRole, "pg-role", "", "PostgreSQL role whose password rotates")
	cmd.Flags().StringVar(&awsRegion, "aws-region", "us-east-1", "AWS region")
	cmd.Flags().StringVar(&awsAccessKey, "aws-access-key", "", "AWS access key ID")
	cmd.Flags().StringVar(&awsSecretKey, "aws-secret-key", "", "AWS secret access key")
	cmd.Flags().StringVar(&awsUser, "aws-user", "", "IAM user whose access key rotates")
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "rotation webhook URL")
	cmd.Flags().StringVar(&webhookSecret, "webhook-secret", "", "webhook HMAC secret")
	return cmd
}

func newRotationRunCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "run POLICY_ID",
		Short: "Run a rotation now",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result rotationRunsResponse
			if err := c.Post(cmd.Context(), rotationBase(resolved)+"/"+url.PathEscape(args[0])+"/run", map[string]any{}, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			if len(result.Runs) == 0 {
				out.Success("Rotation completed.")
				return nil
			}
			out.Success("Rotation %s: %s (%s).", result.Runs[0].Status, result.Runs[0].Message, result.Runs[0].Phase)
			return nil
		},
	}
}

func newRotationToggleCommand(opts *Options, enabled bool) *cobra.Command {
	use := "enable POLICY_ID"
	if !enabled {
		use = "disable POLICY_ID"
	}
	return &cobra.Command{
		Use:   use,
		Short: "Enable or disable a rotation schedule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := c.Post(cmd.Context(), rotationBase(resolved)+"/"+url.PathEscape(args[0])+"/enabled",
				map[string]any{"enabled": enabled}, nil); err != nil {
				return err
			}
			out.Success("Rotation schedule updated.")
			return nil
		},
	}
}

func newRotationRunsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "runs POLICY_ID",
		Short: "Show rotation run history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result rotationRunsResponse
			if err := c.Get(cmd.Context(), rotationBase(resolved)+"/"+url.PathEscape(args[0])+"/runs", &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Runs)
			}
			rows := make([][]string, 0, len(result.Runs))
			for _, run := range result.Runs {
				rows = append(rows, []string{run.StartedAt.Local().Format(time.DateTime), run.Status, run.Phase, run.Message})
			}
			out.Table([]string{"STARTED", "STATUS", "PHASE", "MESSAGE"}, rows)
			return nil
		},
	}
}

func newRotationDeleteCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rm POLICY_ID",
		Short: "Delete a rotation policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := c.Delete(cmd.Context(), rotationBase(resolved)+"/"+url.PathEscape(args[0])); err != nil {
				return err
			}
			out.Success("Deleted rotation policy %s.", args[0])
			return nil
		},
	}
}
