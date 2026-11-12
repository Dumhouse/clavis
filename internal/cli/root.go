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
	root.AddCommand(newAdminCommand(opts))
	root.AddCommand(newMembersCommand(opts))
	root.AddCommand(newIdentitiesCommand(opts))
	root.AddCommand(newSyncsCommand(opts))
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
