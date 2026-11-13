package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newTrustCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Trust a server's self-signed certificate on this computer",
	}
	cmd.AddCommand(newTrustAddCommand(opts), newTrustListCommand(opts), newTrustRemoveCommand(opts))
	return cmd
}

// serverArg is the server from the argument, or from --server/.clavis.yaml.
func serverArg(opts *Options, args []string) (string, error) {
	if len(args) == 1 {
		return normalizeServerURL(args[0]), nil
	}
	resolved, err := opts.resolveContext(false)
	if err != nil {
		return "", err
	}
	return resolved.Server, nil
}

func normalizeServerURL(server string) string {
	if !strings.HasPrefix(server, "https://") && !strings.HasPrefix(server, "http://") {
		server = "https://" + server
	}
	return strings.TrimRight(server, "/")
}

func newTrustAddCommand(opts *Options) *cobra.Command {
	var fingerprint string
	cmd := &cobra.Command{
		Use:   "add [SERVER]",
		Short: "Check a server's certificate and trust it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			server, err := serverArg(opts, args)
			if err != nil {
				return err
			}
			if trustKey(server) == "" {
				return fmt.Errorf("%s is not an https:// URL; there is no certificate to trust", server)
			}
			untrusted := probeServerTLS(cmd.Context(), server)
			if untrusted == nil {
				if err := reachable(cmd.Context(), server); err != nil {
					return fmt.Errorf("could not reach %s: %w", server, err)
				}
				out.Success("%s is already trusted by this computer.", server)
				return nil
			}
			if fingerprint != "" && !isTemporaryCert(untrusted.Leaf) {
				if !fingerprintMatches(untrusted.Leaf, fingerprint) {
					return fmt.Errorf("the certificate of %s does not match --fingerprint (it is %s); nothing was saved",
						server, colonHex(certHash(untrusted.Leaf)))
				}
				if err := pinCertificate(untrusted.Key, untrusted.Leaf); err != nil {
					return err
				}
				out.Success("Trusted %s (fingerprint matches).", server)
				return nil
			}
			return confirmTrust(out, untrusted)
		},
	}
	cmd.Flags().StringVar(&fingerprint, "fingerprint", "", "expected SHA-256 of the certificate or its public key; trusts without asking")
	return cmd
}

func newTrustListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List servers trusted on this computer",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			pins, err := loadTrust()
			if err != nil {
				return err
			}
			if out.JSON {
				return out.PrintJSON(pins)
			}
			if len(pins) == 0 {
				out.Info("No servers trusted yet. `clavis login` asks when it meets a self-signed certificate.")
				return nil
			}
			rows := make([][]string, 0, len(pins))
			for _, key := range sortedTrustKeys(pins) {
				pin := pins[key]
				rows = append(rows, []string{key, colonHex(pin.Certificate)[:23] + "…", pin.NotAfter.Format("2006-01-02"), pin.AddedAt.Format("2006-01-02")})
			}
			out.Table([]string{"SERVER", "SHA-256", "CERT EXPIRES", "TRUSTED ON"}, rows)
			return nil
		},
	}
}

func newTrustRemoveCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rm SERVER",
		Short: "Stop trusting a server's certificate",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			key := trustKey(normalizeServerURL(args[0]))
			pins, err := loadTrust()
			if err != nil {
				return err
			}
			if _, ok := pins[key]; !ok {
				return &ExitError{Code: ExitNotFound, Err: errors.New(args[0] + " is not in the trusted list (see `clavis trust ls`)")}
			}
			delete(pins, key)
			if err := saveTrust(pins); err != nil {
				return err
			}
			out.Success("Removed %s.", key)
			return nil
		},
	}
}

func reachable(ctx context.Context, server string) error {
	hc, err := httpClientFor(server, 15*time.Second)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/healthz", nil)
	if err != nil {
		return err
	}
	response, err := hc.Do(request)
	if err != nil {
		return err
	}
	return response.Body.Close()
}
