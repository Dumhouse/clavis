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
			defer func() { _ = os.Remove(tempPath) }()
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
			if err := os.WriteFile(outPath, encrypted, 0o600); err != nil { //nolint:gosec // G703: output path is chosen by the operator on the command line
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
			defer func() { _ = os.Remove(tempPath) }()
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
				if err := os.WriteFile(cfg.Database.Path, plaintext, 0o600); err != nil { //nolint:gosec // G703: restore target is the configured database path
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

func newAdminBackupVerifyCommand(opts *Options) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "verify-backup FILE",
		Short: "Check that a backup can be decrypted and read, without touching the live database",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
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
				return fmt.Errorf("could not decrypt the backup with this server's root key: %w", err)
			}
			var summary backup.Summary
			if cfg.Database.Driver == "postgres" {
				summary, err = backup.VerifyPostgresDump(plaintext)
			} else {
				summary, err = verifySQLiteBackup(cmd.Context(), plaintext)
			}
			if err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(summary)
			}
			out.Success("Backup %s is readable (%s).", cmd.Flags().Arg(0), summary.Format)
			if summary.Format == "sqlite" {
				out.Info("It holds %d organization(s), %d project(s), %d environment(s) and %d secret(s); the integrity check passed.",
					summary.Orgs, summary.Projects, summary.Environments, summary.Secrets)
			} else {
				out.Info("The dump is complete. For a full test, restore it into a spare Postgres database.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file")
	return cmd
}

// verifySQLiteBackup opens the decrypted copy in a temporary directory, so
// the live database and the backup file are never touched.
func verifySQLiteBackup(ctx context.Context, plaintext []byte) (backup.Summary, error) {
	dir, err := os.MkdirTemp("", "clavis-verify-")
	if err != nil {
		return backup.Summary{}, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "backup.db")
	if err := os.WriteFile(path, plaintext, 0o600); err != nil { //nolint:gosec // G703: a fixed name inside our own temporary directory
		return backup.Summary{}, err
	}
	st, err := store.Open(ctx, path)
	if err != nil {
		return backup.Summary{}, fmt.Errorf("the backup does not open as a Clavis database: %w", err)
	}
	defer func() { _ = st.Close() }()
	// Bring the temporary copy up to this version's schema: a backup from an
	// older release must restore with the binary you run today.
	if err := st.Migrate(ctx); err != nil {
		return backup.Summary{}, fmt.Errorf("the backup does not upgrade to this version's schema: %w", err)
	}
	return backup.VerifySQLite(ctx, st.DB())
}

func loadRootKey(ctx context.Context, cfg *config.Config) (crypto.Key, error) {
	provider, err := rootKeyProvider(cfg)
	if err != nil {
		return crypto.Key{}, err
	}
	defer func() { _ = provider.Close() }()
	return provider.Load(ctx)
}
