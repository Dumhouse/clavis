package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/crypto"
	"github.com/dumhouse/clavis/internal/jobs"
	"github.com/dumhouse/clavis/internal/logging"
	"github.com/dumhouse/clavis/internal/server"
	"github.com/dumhouse/clavis/internal/store"
)

func newServerCommand(opts *Options) *cobra.Command {
	var configPath, role string
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the Clavis server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if role != "all" && role != "api" && role != "worker" {
				return fmt.Errorf("--role must be all, api, or worker")
			}
			return runServer(cmd, opts, configPath, role)
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to clavis.yaml")
	cmd.Flags().StringVar(&role, "role", "all", "process role: all, api, or worker")
	return cmd
}

func runServer(cmd *cobra.Command, opts *Options, configPath, role string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if opts.Debug {
		cfg.Log.Level = "debug"
	}
	logger := logging.New(cmd.ErrOrStderr(), cfg.Log.Level, cfg.Log.Format)

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if cfg.Database.Driver == "sqlite" {
		if dir := filepath.Dir(cfg.Database.Path); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return fmt.Errorf("server: create data directory: %w", err)
			}
		}
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
	defer func() {
		if err := st.Close(); err != nil {
			logger.Error("close database", "error", err)
		}
	}()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if role == "worker" {
		queue := jobs.New(st, logger, jobs.WithWorkerID("worker-"+hostname()))
		return queue.Run(ctx)
	}

	provider, err := rootKeyProvider(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = provider.Close() }()

	rootKey, err := provider.Load(ctx)
	if err != nil {
		return fmt.Errorf("server: load root key: %w", err)
	}
	if cfg.Keys.Provider == "file" {
		logger.Warn("root key loaded from file; back it up and keep it out of the database backups",
			"provider", provider.Name(), "path", cfg.Keys.RootKeyFile)
	}

	srv, err := server.New(cfg, st, logger, rootKey)
	if err != nil {
		return err
	}

	if role != "api" {
		queue := jobs.New(st, logger, jobs.WithWorkerID("api-"+hostname()))
		go func() {
			if err := queue.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("job worker stopped", "error", err)
			}
		}()
	}

	return srv.Run(ctx)
}

func rootKeyProvider(cfg *config.Config) (crypto.Provider, error) {
	switch cfg.Keys.Provider {
	case "file":
		if _, err := os.Stat(cfg.Keys.RootKeyFile + ".sealed"); err == nil {
			return nil, fmt.Errorf("root key is sealed; run `clavis admin unseal` before starting the server")
		}
		return crypto.NewFileProvider(cfg.Keys.RootKeyFile, true), nil
	case "env":
		return crypto.NewEnvProvider(cfg.Keys.RootKeyEnv), nil
	case "aws-kms", "gcp-kms":
		return crypto.NewKMSProvider(crypto.KMSConfig{
			Path: cfg.Keys.RootKeyFile, Provider: cfg.Keys.Provider,
			AWSRegion: cfg.Keys.AWSRegion, AWSKeyID: cfg.Keys.AWSKeyID,
			AWSAccessKey: cfg.Keys.AWSAccessKey, AWSSecretKey: cfg.Keys.AWSSecretKey,
			AWSSession: cfg.Keys.AWSSession, AWSEndpoint: cfg.Keys.AWSEndpoint,
			GCPKey: cfg.Keys.GCPKey, GCPSAFile: cfg.Keys.GCPKeyFile, GCPEndpoint: cfg.Keys.GCPEndpoint,
		}), nil
	default:
		return nil, fmt.Errorf("server: unsupported root key provider %q", cfg.Keys.Provider)
	}
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil || h == "" {
		return "local"
	}
	return h
}
