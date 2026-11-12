package cli

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/store"
)

func newAdminCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Instance administration",
	}
	auditCmd := &cobra.Command{Use: "audit", Short: "Audit log administration"}
	auditCmd.AddCommand(newAuditVerifyCommand(opts))
	auditCmd.AddCommand(newAuditCheckpointCommand(opts))
	cmd.AddCommand(auditCmd)
	cmd.AddCommand(newRotateRootKeyCommand(opts))
	return cmd
}

func newRotateRootKeyCommand(opts *Options) *cobra.Command {
	var configPath string
	var initOnly, yes bool
	cmd := &cobra.Command{
		Use:   "rotate-root-key",
		Short: "Initialise or rotate the instance root key while the server is stopped",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if cfg.Keys.Provider != "file" {
				return fmt.Errorf("root key rotation requires keys.provider=file (current: %s)", cfg.Keys.Provider)
			}
			path := cfg.Keys.RootKeyFile
			if initOnly {
				if _, err := os.Stat(path); err == nil {
					out.Info("Root key already exists at %s", path)
					return nil
				}
				provider := crypto.NewFileProvider(path, true)
				key, err := provider.Load(cmd.Context())
				if err != nil {
					return err
				}
				defer key.Zero()
				out.Success("Created root key at %s with 0600 permissions.", path)
				return nil
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("root key %s does not exist; run `clavis admin rotate-root-key --init` first", path)
			}
			if !yes {
				answer, err := promptLine("This rewrites every org key and MFA secret. Type 'rotate' to continue: ")
				if err != nil {
					return err
				}
				if strings.TrimSpace(answer) != "rotate" {
					return fmt.Errorf("rotation cancelled")
				}
			}
			return rotateRootKey(cmd.Context(), out, cfg, path)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "configuration file (default: CLAVIS_CONFIG or data defaults)")
	cmd.Flags().BoolVar(&initOnly, "init", false, "create the root key if it does not exist")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")
	return cmd
}

func rotateRootKey(ctx context.Context, out *Output, cfg *config.Config, path string) error {
	provider := crypto.NewFileProvider(path, false)
	oldKey, err := provider.Load(ctx)
	if err != nil {
		return err
	}
	defer oldKey.Zero()
	st, err := store.Open(ctx, cfg.Database.Path)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	orgs, err := st.ListAllOrgs(ctx)
	if err != nil {
		return err
	}
	users, err := st.ListUsersWithMFA(ctx)
	if err != nil {
		return err
	}
	newKey, err := crypto.GenerateKey()
	if err != nil {
		return err
	}
	defer newKey.Zero()
	oldKeyring := crypto.NewKeyring(oldKey)
	oldMFA, err := crypto.DeriveMFAKey(oldKey)
	if err != nil {
		return err
	}
	defer oldMFA.Zero()
	newMFA, err := crypto.DeriveMFAKey(newKey)
	if err != nil {
		return err
	}
	defer newMFA.Zero()

	orgUpdates := make([]store.OrgKeyRotation, 0, len(orgs))
	for _, org := range orgs {
		wrapped, err := oldKeyring.RewrapOrgKey(org.ID, org.WrappedOrgKey, newKey)
		if err != nil {
			return fmt.Errorf("rewrap org %s: %w", org.Slug, err)
		}
		orgUpdates = append(orgUpdates, store.OrgKeyRotation{
			OrgID: org.ID, Wrapped: wrapped, KeyVersion: org.KeyVersion,
		})
	}
	userUpdates := make([]store.UserMFARotation, 0, len(users))
	for _, user := range users {
		ciphertext, err := authn.ReencryptMFASecret(user.ID, user.MFASecretCiphertext, oldMFA, newMFA)
		if err != nil {
			return fmt.Errorf("rotate MFA secret for %s: %w", user.Email, err)
		}
		userUpdates = append(userUpdates, store.UserMFARotation{
			UserID: user.ID, Ciphertext: ciphertext, Enabled: user.MFAEnabled,
		})
	}

	backup := path + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
	if err := copyFile(path, backup); err != nil {
		return err
	}
	if err := provider.Save(ctx, newKey); err != nil {
		return err
	}
	if err := st.ApplyRootKeyRotation(ctx, orgUpdates, userUpdates); err != nil {
		// Put the old key back so the untouched database remains usable.
		_ = copyFile(backup, path)
		return err
	}
	out.Success("Rotated root key: %d org(s), %d MFA secret(s).", len(orgUpdates), len(userUpdates))
	out.Info("Previous key backed up to %s. Keep it until every backup is retired.", backup)
	return nil
}

func copyFile(src, dst string) error {
	from, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = from.Close() }()
	to, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = to.Close() }()
	if _, err := io.Copy(to, from); err != nil {
		return err
	}
	if err := to.Sync(); err != nil {
		return err
	}
	if err := to.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, 0o600)
}

type auditVerifyResult struct {
	ChainValid  bool   `json:"chain_valid"`
	Checkpoints int    `json:"checkpoints"`
	LastSeq     int64  `json:"last_seq"`
	PublicKey   string `json:"public_key,omitempty"`
	Problem     string `json:"problem,omitempty"`
}

func (o *Options) auditContext() (*resolvedContext, error) {
	resolved, err := o.resolveContext(false)
	if err != nil {
		return nil, err
	}
	if resolved.Org == "" {
		return nil, fmt.Errorf("no organization: run `clavis init` or pass --org")
	}
	return resolved, nil
}

func newAuditVerifyCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Verify the audit hash chain and signed checkpoints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.auditContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result auditVerifyResult
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/audit-events/verify"
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				if err := out.PrintJSON(result); err != nil {
					return err
				}
			} else if result.ChainValid {
				out.Success("Audit chain verified (%d events, %d checkpoints).", result.LastSeq, result.Checkpoints)
				if result.PublicKey != "" {
					out.Info("Checkpoint key: %s", result.PublicKey)
				}
			} else {
				out.Errorf("Audit verification failed: %s", result.Problem)
			}
			if !result.ChainValid {
				return &ExitError{Code: ExitGeneral, Err: fmt.Errorf("audit verification failed"), Silent: true}
			}
			return nil
		},
	}
}

func newAuditCheckpointCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "checkpoint",
		Short: "Sign the current audit chain head",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.auditContext()
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var checkpoint struct {
				Seq       int64  `json:"seq"`
				EventHash string `json:"event_hash"`
			}
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/audit-events/checkpoint"
			if err := c.Post(cmd.Context(), path, map[string]any{}, &checkpoint); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(checkpoint)
			}
			hash := checkpoint.EventHash
			if len(hash) > 12 {
				hash = hash[:12] + "…"
			}
			out.Success("Signed checkpoint at sequence %d (%s).", checkpoint.Seq, strings.TrimSuffix(hash, "…"))
			return nil
		},
	}
}
