package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
)

type sealedKeyMarker struct {
	Threshold int    `json:"threshold"`
	Shares    int    `json:"shares"`
	SHA256    string `json:"sha256"`
	CreatedAt string `json:"created_at"`
}

func newAdminShamirCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "shamir", Short: "Split the root key into Shamir shares"}
	cmd.AddCommand(newAdminShamirSplitCommand(opts))
	return cmd
}

func newAdminShamirSplitCommand(opts *Options) *cobra.Command {
	var configPath, outDir string
	var shares, threshold int
	var seal, yes bool
	cmd := &cobra.Command{
		Use:   "split",
		Short: "Split the root key into threshold shares",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if cfg.Keys.Provider != "file" {
				return fmt.Errorf("Shamir unseal requires keys.provider=file")
			}
			if seal && !yes {
				return fmt.Errorf("--seal deletes the root key file; re-run with --yes after securing the shares")
			}
			provider := crypto.NewFileProvider(cfg.Keys.RootKeyFile, false)
			rootKey, err := provider.Load(cmd.Context())
			if err != nil {
				return err
			}
			defer rootKey.Zero()
			parts, err := crypto.SplitSecret(rootKey[:], shares, threshold)
			if err != nil {
				return err
			}
			if outDir == "" {
				outDir = "."
			}
			if err := os.MkdirAll(outDir, 0o700); err != nil {
				return err
			}
			for i, part := range parts {
				path := filepath.Join(outDir, fmt.Sprintf("root-share-%02d.key", i+1))
				if err := os.WriteFile(path, part, 0o600); err != nil {
					return err
				}
				out.Info("Wrote %s", path)
			}
			sum := sha256.Sum256(rootKey[:])
			marker := sealedKeyMarker{
				Threshold: threshold, Shares: shares, SHA256: hex.EncodeToString(sum[:]),
				CreatedAt: time.Now().UTC().Format(time.RFC3339),
			}
			if seal {
				raw, _ := json.MarshalIndent(marker, "", "  ")
				markerPath := cfg.Keys.RootKeyFile + ".sealed"
				if err := os.WriteFile(markerPath, raw, 0o600); err != nil {
					return err
				}
				if err := os.Remove(cfg.Keys.RootKeyFile); err != nil {
					return err
				}
				out.Warn("Root key removed. The server will refuse to start until `clavis admin unseal` succeeds.")
			}
			out.Success("Created %d shares with a threshold of %d.", shares, threshold)
			out.Warn("Shares are equivalent to the root key. Store them separately, offline, and access-controlled.")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file")
	cmd.Flags().StringVar(&outDir, "out", ".", "directory for share files")
	cmd.Flags().IntVar(&shares, "shares", 5, "number of shares")
	cmd.Flags().IntVar(&threshold, "threshold", 3, "shares required to unseal")
	cmd.Flags().BoolVar(&seal, "seal", false, "remove the root key file after writing shares")
	cmd.Flags().BoolVar(&yes, "yes", false, "confirm sealing")
	return cmd
}

func newAdminUnsealCommand(opts *Options) *cobra.Command {
	var configPath string
	var sharePaths []string
	var force bool
	cmd := &cobra.Command{
		Use:   "unseal",
		Short: "Reconstruct the root key from Shamir shares",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if cfg.Keys.Provider != "file" {
				return fmt.Errorf("Shamir unseal requires keys.provider=file")
			}
			markerPath := cfg.Keys.RootKeyFile + ".sealed"
			raw, err := os.ReadFile(markerPath)
			if err != nil {
				return fmt.Errorf("root key is not sealed (missing %s)", markerPath)
			}
			var marker sealedKeyMarker
			if err := json.Unmarshal(raw, &marker); err != nil {
				return fmt.Errorf("invalid sealed marker: %w", err)
			}
			if len(sharePaths) < marker.Threshold {
				return fmt.Errorf("need at least %d shares, got %d", marker.Threshold, len(sharePaths))
			}
			shares := make([][]byte, 0, len(sharePaths))
			for _, path := range sharePaths {
				data, err := os.ReadFile(path)
				if err != nil {
					return fmt.Errorf("read share %s: %w", path, err)
				}
				shares = append(shares, data)
			}
			keyBytes, err := crypto.CombineShares(shares, marker.Threshold)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(keyBytes)
			if hex.EncodeToString(sum[:]) != marker.SHA256 {
				return fmt.Errorf("shares did not reconstruct the expected root key")
			}
			if _, err := os.Stat(cfg.Keys.RootKeyFile); err == nil && !force {
				return fmt.Errorf("%s already exists; pass --force to overwrite", cfg.Keys.RootKeyFile)
			}
			var key crypto.Key
			copy(key[:], keyBytes)
			defer key.Zero()
			if err := crypto.NewFileProvider(cfg.Keys.RootKeyFile, false).Save(cmd.Context(), key); err != nil {
				return err
			}
			_ = os.Remove(markerPath)
			out.Success("Unsealed root key at %s. Start the server now.", cfg.Keys.RootKeyFile)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file")
	cmd.Flags().StringArrayVar(&sharePaths, "share", nil, "share file (repeat for each share)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing root key file")
	return cmd
}
