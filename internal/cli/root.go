// Package cli implements the clavis command tree. The CLI and server share one
// binary.
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Options are global flags shared by all commands.
type Options struct {
	JSON      bool
	ServerURL string
	Debug     bool
	Org       string
	Project   string
	Env       string
	Path      string
	Overlay   string
	Output    *Output
}

func (o *Options) output() *Output {
	if o.Output != nil {
		return o.Output
	}
	return NewOutput(o.JSON)
}

// NewRootCommand builds the command tree.
func NewRootCommand() *cobra.Command {
	return NewRootCommandWithOptions(&Options{})
}

// NewRootCommandWithOptions builds the tree with injected options, used by
// tests and embedders.
func NewRootCommandWithOptions(opts *Options) *cobra.Command {
	root := &cobra.Command{
		Use:           "clavis",
		Short:         "Open-source secrets management",
		Long:          "Clavis stores application secrets, shares them safely, and injects them into your apps, CI, and agents.",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}
	root.PersistentFlags().BoolVar(&opts.JSON, "json", opts.JSON, "output machine-readable JSON")
	root.PersistentFlags().StringVar(&opts.ServerURL, "server", opts.ServerURL, "Clavis server URL (default: from .clavis.yaml or saved login)")
	root.PersistentFlags().BoolVar(&opts.Debug, "debug", opts.Debug, "enable debug logging")
	root.PersistentFlags().StringVar(&opts.Org, "org", opts.Org, "organization slug")
	root.PersistentFlags().StringVar(&opts.Project, "project", opts.Project, "project slug")
	root.PersistentFlags().StringVar(&opts.Env, "env", opts.Env, "environment slug")
	root.PersistentFlags().StringVar(&opts.Path, "path", opts.Path, "folder path (default /)")
	root.PersistentFlags().StringVar(&opts.Overlay, "overlay", opts.Overlay, "tenant/region overlay slug")

	root.AddCommand(newVersionCommand(opts))
	root.AddCommand(newServerCommand(opts))
	root.AddCommand(newLoginCommand(opts))
	root.AddCommand(newLogoutCommand(opts))
	root.AddCommand(newWhoamiCommand(opts))
	root.AddCommand(newInitCommand(opts))
	root.AddCommand(newLinkCommand(opts))
	root.AddCommand(newUnlinkCommand(opts))
	root.AddCommand(newSecretsCommand(opts))
	root.AddCommand(newEnvCommand(opts))
	root.AddCommand(newRunCommand(opts))
	root.AddCommand(newExportCommand(opts))
	root.AddCommand(newImportCommand(opts))
	root.AddCommand(newCheckCommand(opts))
	root.AddCommand(newPromoteCommand(opts))
	root.AddCommand(newShellCommand(opts))
	root.AddCommand(newAuditCommand(opts))
	root.AddCommand(newScanCommand(opts))
	root.AddCommand(newShareCommand(opts))
	root.AddCommand(newLeaseCommand(opts))
	root.AddCommand(newHealthCommand(opts))
	root.AddCommand(newUpgradeCommand(opts))
	root.AddCommand(newRotationCommand(opts))
	root.AddCommand(newSchemaCommand(opts))
	root.AddCommand(newCodegenCommand(opts))
	root.AddCommand(newAdminCommand(opts))
	root.AddCommand(newMembersCommand(opts))
	root.AddCommand(newIdentitiesCommand(opts))
	root.AddCommand(newAgentsCommand(opts))
	root.AddCommand(newMCPCommand(opts))
	root.AddCommand(newSyncsCommand(opts))
	root.AddCommand(newConnectionsCommand(opts))
	root.AddCommand(newSetsCommand(opts))
	root.AddCommand(newAccessCommand(opts))
	return root
}

// Execute runs the CLI and returns a stable exit code.
func Execute() int {
	root := NewRootCommand()
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	if err := root.Execute(); err != nil {
		var exit *ExitError
		if errors.As(err, &exit) && exit.Silent {
			return exit.Code
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return ExitCode(err)
	}
	return ExitOK
}
