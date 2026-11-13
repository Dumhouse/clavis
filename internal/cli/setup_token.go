package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
)

func newSetupTokenCommand(_ *Options) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "setup-token",
		Short: "Print the one-time first-run setup token",
		Long: `Print the one-time token that claims a fresh server.

Run it where the server's data directory is available, for example:

  docker exec clavis clavis setup-token

The token file is deleted once the owner account exists.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			path := filepath.Join(filepath.Dir(cfg.Database.Path), "setup-token")
			raw, err := os.ReadFile(path) //nolint:gosec // G304: path derives from the operator's own configuration
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("no setup token at %s: setup is already complete, or the server has not started yet", path)
			}
			if err != nil {
				return fmt.Errorf("read setup token: %w", err)
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSpace(string(raw)))
			return err
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to clavis.yaml")
	return cmd
}
