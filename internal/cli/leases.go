package cli

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type dynamicConfigJSON struct {
	ID         string    `json:"id"`
	Engine     string    `json:"engine"`
	DefaultTTL int       `json:"default_ttl_seconds"`
	MaxTTL     int       `json:"max_ttl_seconds"`
	CreatedAt  time.Time `json:"created_at"`
}

type dynamicConfigsResponse struct {
	Configs []dynamicConfigJSON `json:"configs"`
}

type leaseJSON struct {
	ID         string     `json:"id"`
	ConfigID   string     `json:"config_id"`
	IdentityID string     `json:"identity_id,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type leaseResponse struct {
	Lease   leaseJSON         `json:"lease"`
	Secrets map[string]string `json:"secrets"`
}

type leasesResponse struct {
	Leases []leaseJSON `json:"leases"`
}

func newLeaseCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lease",
		Short: "Create and manage dynamic secret leases",
	}
	cmd.AddCommand(newLeaseConfigsCommand(opts))
	cmd.AddCommand(newLeaseConfigCreateCommand(opts))
	cmd.AddCommand(newLeaseCreateCommand(opts))
	cmd.AddCommand(newLeaseListCommand(opts))
	cmd.AddCommand(newLeaseRenewCommand(opts))
	cmd.AddCommand(newLeaseRevokeCommand(opts))
	cmd.AddCommand(newLeaseRunCommand(opts))
	return cmd
}

func leaseConfigsPath(resolved *resolvedContext) string {
	return projectBase(resolved) + "/dynamic-configs"
}

func leaseConfigPath(resolved *resolvedContext, id string) string {
	return leaseConfigsPath(resolved) + "/" + url.PathEscape(id)
}

func newLeaseConfigsCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "configs",
		Short: "List dynamic secret configs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result dynamicConfigsResponse
			if err := c.Get(cmd.Context(), leaseConfigsPath(resolved), &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Configs)
			}
			rows := make([][]string, 0, len(result.Configs))
			for _, config := range result.Configs {
				rows = append(rows, []string{config.ID, config.Engine, fmt.Sprintf("%ds", config.DefaultTTL), fmt.Sprintf("%ds", config.MaxTTL)})
			}
			out.Table([]string{"ID", "ENGINE", "DEFAULT TTL", "MAX TTL"}, rows)
			return nil
		},
	}
}

func newLeaseConfigCreateCommand(opts *Options) *cobra.Command {
	var engine, host, database, username, password, sslmode, grantRole string
	var port, defaultTTL, maxTTL int
	var awsRegion, awsAccessKey, awsSecretKey, awsRoleARN, awsExternalID, awsSessionName string
	cmd := &cobra.Command{
		Use:   "config-create",
		Short: "Create a PostgreSQL or AWS STS dynamic secret config",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			config := map[string]any{
				"host": host, "port": port, "database": database, "username": username,
				"password": password, "sslmode": sslmode, "grant_role": grantRole,
			}
			if engine == "aws" {
				config = map[string]any{
					"region": awsRegion, "access_key": awsAccessKey, "secret_key": awsSecretKey,
					"role_arn": awsRoleARN, "external_id": awsExternalID, "session_name": awsSessionName,
				}
			}
			var created dynamicConfigJSON
			if err := c.Post(cmd.Context(), leaseConfigsPath(resolved), map[string]any{
				"engine": engine, "config": config,
				"default_ttl_seconds": defaultTTL, "max_ttl_seconds": maxTTL,
			}, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Created %s dynamic config %s.", created.Engine, created.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&engine, "engine", "postgres", "dynamic engine (postgres)")
	cmd.Flags().StringVar(&host, "host", "localhost", "PostgreSQL host")
	cmd.Flags().IntVar(&port, "port", 5432, "PostgreSQL port")
	cmd.Flags().StringVar(&database, "database", "", "PostgreSQL database")
	cmd.Flags().StringVar(&username, "username", "", "admin username")
	cmd.Flags().StringVar(&password, "password", "", "admin password")
	cmd.Flags().StringVar(&sslmode, "sslmode", "disable", "PostgreSQL sslmode")
	cmd.Flags().StringVar(&grantRole, "grant-role", "", "role granted to generated users")
	cmd.Flags().StringVar(&awsRegion, "aws-region", "us-east-1", "AWS region")
	cmd.Flags().StringVar(&awsAccessKey, "aws-access-key", "", "AWS access key ID")
	cmd.Flags().StringVar(&awsSecretKey, "aws-secret-key", "", "AWS secret access key")
	cmd.Flags().StringVar(&awsRoleARN, "aws-role-arn", "", "role ARN to assume")
	cmd.Flags().StringVar(&awsExternalID, "aws-external-id", "", "STS external ID")
	cmd.Flags().StringVar(&awsSessionName, "aws-session-name", "", "STS session name")
	cmd.Flags().IntVar(&defaultTTL, "default-ttl", 3600, "default lease TTL in seconds")
	cmd.Flags().IntVar(&maxTTL, "max-ttl", 86400, "maximum lease TTL in seconds")
	return cmd
}

func newLeaseCreateCommand(opts *Options) *cobra.Command {
	var ttl int
	cmd := &cobra.Command{
		Use:   "create CONFIG_ID",
		Short: "Create dynamic credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result leaseResponse
			if err := c.Post(cmd.Context(), leaseConfigPath(resolved, args[0])+"/leases", map[string]any{
				"ttl_seconds": ttl,
			}, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			out.Success("Lease %s expires %s.", result.Lease.ID, result.Lease.ExpiresAt.Local().Format(time.DateTime))
			for _, key := range []string{"username", "password", "host", "port", "database"} {
				if value := result.Secrets[key]; value != "" {
					out.Info("%s=%s", strings.ToUpper(key), value)
				}
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 0, "lease TTL in seconds (default config TTL)")
	return cmd
}

func newLeaseListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls CONFIG_ID",
		Short: "List leases for a config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result leasesResponse
			if err := c.Get(cmd.Context(), leaseConfigPath(resolved, args[0])+"/leases", &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Leases)
			}
			rows := make([][]string, 0, len(result.Leases))
			for _, lease := range result.Leases {
				status := "active"
				if lease.RevokedAt != nil {
					status = "revoked"
				} else if lease.ExpiresAt.Before(time.Now()) {
					status = "expired"
				}
				rows = append(rows, []string{lease.ID, status, lease.ExpiresAt.Local().Format(time.DateTime)})
			}
			out.Table([]string{"LEASE", "STATUS", "EXPIRES"}, rows)
			return nil
		},
	}
}

func newLeaseRenewCommand(opts *Options) *cobra.Command {
	var ttl int
	cmd := &cobra.Command{
		Use:   "renew LEASE_ID",
		Short: "Renew a lease",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result leaseResponse
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/leases/"+url.PathEscape(args[0])+"/renew",
				map[string]any{"ttl_seconds": ttl}, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			out.Success("Lease %s now expires %s.", result.Lease.ID, result.Lease.ExpiresAt.Local().Format(time.DateTime))
			return nil
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 0, "new TTL in seconds")
	return cmd
}

func newLeaseRevokeCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke LEASE_ID",
		Short: "Revoke a lease and drop its credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/leases/"+url.PathEscape(args[0])+"/revoke", map[string]any{}, nil); err != nil {
				return err
			}
			out.Success("Revoked lease %s.", args[0])
			return nil
		},
	}
}

func newLeaseRunCommand(opts *Options) *cobra.Command {
	var ttl int
	cmd := &cobra.Command{
		Use:   "run CONFIG_ID -- COMMAND [ARGS...]",
		Short: "Run a command with dynamic credentials and auto-revoke",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result leaseResponse
			if err := c.Post(cmd.Context(), leaseConfigPath(resolved, args[0])+"/leases", map[string]any{
				"ttl_seconds": ttl,
			}, &result); err != nil {
				return err
			}
			defer func() {
				_ = c.Post(context.Background(), projectBase(resolved)+"/leases/"+url.PathEscape(result.Lease.ID)+"/revoke", map[string]any{}, nil)
			}()
			commandArgs := args[1:]
			if commandArgs[0] == "--" {
				commandArgs = commandArgs[1:]
			}
			if len(commandArgs) == 0 {
				return fmt.Errorf("pass a command after --")
			}
			child := exec.CommandContext(cmd.Context(), commandArgs[0], commandArgs[1:]...)
			child.Env = append(os.Environ(),
				"PGHOST="+result.Secrets["host"],
				"PGPORT="+result.Secrets["port"],
				"PGUSER="+result.Secrets["username"],
				"PGPASSWORD="+result.Secrets["password"],
				"PGDATABASE="+result.Secrets["database"],
			)
			child.Stdin = os.Stdin
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			return child.Run()
		},
	}
	cmd.Flags().IntVar(&ttl, "ttl", 0, "lease TTL in seconds")
	return cmd
}
