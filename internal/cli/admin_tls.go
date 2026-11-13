package cli

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/config"
	"github.com/dumhouse/clavis/internal/server"
)

func newAdminTLSInfoCommand(opts *Options) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "tls-info",
		Short: "Show the server's HTTPS certificate and its fingerprint",
		Long: `Show which certificate the server presents, with the SHA-256 fingerprint
people compare when the CLI or a browser asks whether to trust it.

Run it where the server's data and config are, for example inside the container.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			info, err := server.DescribeTLS(cfg)
			if err != nil {
				return err
			}
			base := strings.TrimRight(info.BaseURL, "/")
			if out.JSON {
				payload := map[string]any{"mode": info.Mode, "base_url": base, "cert_file": info.CertFile, "note": info.Note}
				if info.Leaf != nil {
					payload["sha256"] = info.Fingerprint()
					payload["public_key_sha256"] = info.PublicKeyFingerprint()
					payload["not_after"] = info.Leaf.NotAfter.Format(time.RFC3339)
					payload["names"] = certNames(info)
				}
				return out.PrintJSON(payload)
			}
			out.Info("  Mode          %s", info.Mode)
			out.Info("  Base URL      %s", base)
			if info.Leaf != nil {
				issuer := info.Leaf.Issuer.String()
				if issuer == info.Leaf.Subject.String() {
					issuer = "itself (self-signed)"
				}
				out.Info("  Certificate   %s", info.CertFile)
				out.Info("  Issued to     %s", strings.Join(certNames(info), ", "))
				out.Info("  Issued by     %s", issuer)
				out.Info("  Valid until   %s", info.Leaf.NotAfter.Format("2006-01-02"))
				out.Info("  SHA-256       %s", info.Fingerprint())
				out.Info("  Public key    %s", info.PublicKeyFingerprint())
			}
			if info.Note != "" {
				out.Info("")
				out.Info("%s", info.Note)
			}
			if info.Leaf != nil && info.Leaf.Issuer.String() == info.Leaf.Subject.String() {
				out.Info("")
				out.Info("People connect with `clavis login --server %s`: the CLI shows this SHA-256", base)
				out.Info("and asks once. For scripts and CI:")
				out.Info("  clavis trust add %s --fingerprint %s", base, info.Fingerprint())
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&configPath, "config", "c", "", "path to clavis.yaml")
	return cmd
}

func certNames(info *server.TLSInfo) []string {
	names := append([]string{}, info.Leaf.DNSNames...)
	for _, ip := range info.Leaf.IPAddresses {
		names = append(names, ip.String())
	}
	return names
}
