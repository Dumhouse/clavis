package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
)

// newHealthcheckCommand probes the server running on this machine. It exists
// for container HEALTHCHECKs: the image has no shell or curl, so the binary
// has to do the request itself.
func newHealthcheckCommand(opts *Options) *cobra.Command {
	var configPath string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Check that the server on this machine is ready",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			url := localReadyURL(cfg)
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			if err := probeReady(ctx, url); err != nil {
				return err
			}
			opts.output().Info("ready: %s", url)
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to clavis.yaml")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Second, "how long to wait for an answer")
	return cmd
}

// localReadyURL is the readiness URL on this machine. It always uses the plain
// HTTP port: with TLS on, that port answers /healthz and /readyz itself and only
// redirects everything else, so the probe needs no certificate.
func localReadyURL(cfg *config.Config) string {
	return "http://" + loopbackAddr(cfg.Server.Addr) + "/readyz"
}

// loopbackAddr turns a listen address such as ":8080" or "0.0.0.0:8080" into
// one this machine can dial. A specific bind address is kept.
func loopbackAddr(listen string) string {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return listen
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

// probeReady asks /readyz and fails unless it answers 200. A redirect is not an
// answer: it means the request reached something other than the probe endpoint.
func probeReady(ctx context.Context, url string) error {
	client := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("not ready: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s answered %d", url, resp.StatusCode)
	}
	return nil
}
