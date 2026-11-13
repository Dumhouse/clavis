package cli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/authn"
	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/service"
	"github.com/dumhouse/clavis/internal/store"
)

func newAdminResetPasswordCommand(opts *Options) *cobra.Command {
	var configPath string
	var validFor time.Duration
	cmd := &cobra.Command{
		Use:   "reset-password EMAIL",
		Short: "Print a one-time password reset link for a user (no email needed)",
		Long: `Create a password reset link for a user and print it.

Use it when someone forgot their password and the server has no SMTP, for
example in a home lab. Run it where the server's data and config are, such as
inside the container. The link works once, expires after --valid-for, and the
request is recorded in the audit log of every organization the user belongs to.`,
		Example: `  docker exec clavis clavis admin reset-password you@example.com
  clavis admin reset-password you@example.com --valid-for 2h`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			out := opts.output()
			if validFor <= 0 || validFor > 7*24*time.Hour {
				return errors.New("--valid-for must be between 1m and 168h")
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			var st *store.Store
			if cfg.Database.Driver == "postgres" {
				st, err = store.OpenPostgres(ctx, cfg.Database.DSN)
			} else {
				st, err = store.Open(ctx, cfg.Database.Path)
			}
			if err != nil {
				return err
			}
			defer func() { _ = st.Close() }()
			if err := st.Migrate(ctx); err != nil {
				return err
			}
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			auth, err := authn.NewService(st, logger, filepath.Join(filepath.Dir(cfg.Database.Path), "setup-token"))
			if err != nil {
				return err
			}
			token, user, err := auth.AdminResetLink(ctx, args[0], validFor)
			if errors.Is(err, store.ErrNotFound) {
				return fmt.Errorf("no user with email %s", args[0])
			}
			if err != nil {
				return err
			}

			// Audit: the server's keyring signs the audit chain.
			provider, err := rootKeyProvider(cfg)
			if err != nil {
				return err
			}
			defer func() { _ = provider.Close() }()
			rootKey, err := provider.Load(ctx)
			if err != nil {
				return fmt.Errorf("load root key: %w", err)
			}
			svc := service.New(st, crypto.NewKeyring(rootKey), logger, nil)
			svc.RecordAuthEvent(ctx, user, "auth.password_reset_requested", "admin_cli", service.AuditMeta{})

			link := cfg.Server.BaseURL + "/reset?token=" + url.QueryEscape(token)
			if opts.JSON {
				return out.PrintJSON(map[string]any{"email": user.Email, "url": link, "expires_in": validFor.String()})
			}
			out.Success("Reset link for %s (valid for %s, works once):", user.Email, validFor)
			_, err = fmt.Fprintln(cmd.OutOrStdout(), link)
			if err == nil {
				out.Info("Send it to the user privately. Their other sessions end when they set the new password.")
			}
			return err
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to clavis.yaml")
	cmd.Flags().DurationVar(&validFor, "valid-for", 24*time.Hour, "how long the link stays valid (max 168h)")
	return cmd
}
