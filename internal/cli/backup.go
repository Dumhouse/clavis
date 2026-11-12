package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/backup"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

func newAdminBackupCommand(opts *Options) *cobra.Command {
	var configPath, outPath string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Create an encrypted consistent database backup",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if outPath == "" {
				return fmt.Errorf("pass --out FILE")
			}
			rootKey, err := loadRootKey(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer rootKey.Zero()
			temp, err := os.CreateTemp("", "clavis-backup-*")
			if err != nil {
				return err
			}
			tempPath := temp.Name()
			_ = temp.Close()
			defer os.Remove(tempPath)
			if cfg.Database.Driver == "postgres" {
				if err := backup.SnapshotPostgres(cmd.Context(), cfg.Database.DSN, tempPath); err != nil {
					return err
				}
			} else {
				st, err := store.Open(cmd.Context(), cfg.Database.Path)
				if err != nil {
					return err
				}
				execErr := backup.SnapshotSQLite(cmd.Context(), st.DB(), tempPath)
				_ = st.Close()
				if execErr != nil {
					return execErr
				}
			}
			plaintext, err := os.ReadFile(tempPath)
			if err != nil {
				return err
			}
			encrypted, err := backup.Encrypt(rootKey[:], plaintext)
			if err != nil {
				return err
			}
			if err := os.WriteFile(outPath, encrypted, 0o600); err != nil {
				return err
			}
			out.Success("Wrote encrypted backup to %s.", outPath)
			out.Warn("The backup contains wrapped org keys. Store the root key separately; it is required to restore.")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output backup file")
	return cmd
}

func newAdminRestoreCommand(opts *Options) *cobra.Command {
	var configPath string
	var yes bool
	cmd := &cobra.Command{
		Use:   "restore FILE",
		Short: "Restore an encrypted database backup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if !yes {
				return fmt.Errorf("restore replaces the database; re-run with --yes while the server is stopped")
			}
			rootKey, err := loadRootKey(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			defer rootKey.Zero()
			encrypted, err := os.ReadFile(cmd.Flags().Arg(0))
			if err != nil {
				return err
			}
			plaintext, err := backup.Decrypt(rootKey[:], encrypted)
			if err != nil {
				return err
			}
			temp, err := os.CreateTemp("", "clavis-restore-*")
			if err != nil {
				return err
			}
			tempPath := temp.Name()
			defer os.Remove(tempPath)
			if _, err := temp.Write(plaintext); err != nil {
				_ = temp.Close()
				return err
			}
			_ = temp.Close()
			if cfg.Database.Driver == "postgres" {
				if err := backup.RestorePostgres(cmd.Context(), cfg.Database.DSN, tempPath); err != nil {
					return err
				}
			} else {
				backupPath := cfg.Database.Path + ".bak-" + filepath.Base(tempPath)
				if err := os.Rename(cfg.Database.Path, backupPath); err != nil && !os.IsNotExist(err) {
					return err
				}
				if err := os.WriteFile(cfg.Database.Path, plaintext, 0o600); err != nil {
					return err
				}
				out.Info("Previous database moved to %s", backupPath)
			}
			out.Success("Restored backup from %s.", cmd.Flags().Arg(0))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm replacing the database")
	return cmd
}

func loadRootKey(ctx context.Context, cfg *config.Config) (crypto.Key, error) {
	provider, err := rootKeyProvider(cfg)
	if err != nil {
		return crypto.Key{}, err
	}
	defer func() { _ = provider.Close() }()
	return provider.Load(ctx)
}
