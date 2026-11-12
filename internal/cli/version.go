package cli

import (
	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/version"
)

func newVersionCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			if opts.JSON {
				return out.PrintJSON(map[string]string{
					"version": version.Version,
					"commit":  version.Commit,
					"date":    version.Date,
				})
			}
			out.Info("clavis %s", version.Version)
			out.Info("commit: %s", version.Commit)
			out.Info("built:  %s", version.Date)
			return nil
		},
	}
}
