package cli

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

const backupMagic = "CLVBK1\n"

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
				if err := dumpPostgres(cmd.Context(), cfg.Database.DSN, tempPath); err != nil {
					return err
				}
			} else {
				st, err := store.Open(cmd.Context(), cfg.Database.Path)
				if err != nil {
					return err
				}
				_, execErr := st.DB().ExecContext(cmd.Context(), "VACUUM INTO ?", tempPath)
				_ = st.Close()
				if execErr != nil {
					return fmt.Errorf("sqlite backup: %w", execErr)
				}
			}
			plaintext, err := os.ReadFile(tempPath)
			if err != nil {
				return err
			}
			encrypted, err := encryptBackup(rootKey[:], plaintext)
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
			plaintext, err := decryptBackup(rootKey[:], encrypted)
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
				if err := restorePostgres(cmd.Context(), cfg.Database.DSN, tempPath); err != nil {
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

func backupCipher(key []byte) (cipher.AEAD, error) {
	sum := sha256.Sum256(append(append([]byte(nil), key...), []byte("clavis-backup-v1")...))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptBackup(key, plaintext []byte) ([]byte, error) {
	gcm, err := backupCipher(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(append([]byte(backupMagic), nonce...), nonce, plaintext, []byte(backupMagic)), nil
}

func decryptBackup(key, data []byte) ([]byte, error) {
	if !strings.HasPrefix(string(data), backupMagic) {
		return nil, fmt.Errorf("not a Clavis backup")
	}
	gcm, err := backupCipher(key)
	if err != nil {
		return nil, err
	}
	payload := data[len(backupMagic):]
	if len(payload) < gcm.NonceSize() {
		return nil, fmt.Errorf("truncated backup")
	}
	return gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], []byte(backupMagic))
}

func dumpPostgres(ctx context.Context, dsn, path string) error {
	command := exec.CommandContext(ctx, "pg_dump", "--dbname", dsn, "--file", path, "--no-owner", "--no-privileges")
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("pg_dump: %w (is pg_dump installed?)", err)
	}
	return nil
}

func restorePostgres(ctx context.Context, dsn, path string) error {
	command := exec.CommandContext(ctx, "psql", "--dbname", dsn, "--file", path)
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("psql: %w (is psql installed?)", err)
	}
	return nil
}
