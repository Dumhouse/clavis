package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newUpgradeCommand(opts *Options) *cobra.Command {
	var bundle, identity, issuer string
	var install bool
	cmd := &cobra.Command{
		Use:   "upgrade ARTIFACT",
		Short: "Verify a signed release artifact and install it",
		Long: "Verify a Clavis release with cosign before replacing the current binary.\n" +
			"Requires cosign on PATH and a Sigstore bundle from the GitHub release.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			if bundle == "" {
				bundle = args[0] + ".sigstore.json"
			}
			if identity == "" {
				identity = "https://github.com/dumhouse/clavis/.*"
			}
			if issuer == "" {
				issuer = "https://token.actions.githubusercontent.com"
			}
			cosign, err := exec.LookPath("cosign")
			if err != nil {
				return fmt.Errorf("cosign is required to verify releases: %w", err)
			}
			verify := exec.CommandContext(cmd.Context(), cosign, "verify-blob",
				"--bundle", bundle,
				"--certificate-identity-regexp", identity,
				"--certificate-oidc-issuer", issuer,
				args[0],
			)
			verify.Stdout = out.Out
			verify.Stderr = out.Err
			if err := verify.Run(); err != nil {
				return fmt.Errorf("release signature verification failed: %w", err)
			}
			if !install {
				out.Success("Verified %s. Re-run with --install to replace the current binary.", args[0])
				return nil
			}
			current, err := os.Executable()
			if err != nil {
				return err
			}
			current, err = filepath.EvalSymlinks(current)
			if err != nil {
				return err
			}
			temp, err := installArtifact(args[0])
			if err != nil {
				return err
			}
			defer os.Remove(temp)
			if err := os.Chmod(temp, 0o755); err != nil {
				return err
			}
			if err := os.Rename(temp, current); err != nil {
				return err
			}
			out.Success("Installed verified binary at %s.", current)
			return nil
		},
	}
	cmd.Flags().StringVar(&bundle, "bundle", "", "Sigstore bundle (default ARTIFACT.sigstore.json)")
	cmd.Flags().StringVar(&identity, "identity", "", "certificate identity regexp")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer")
	cmd.Flags().BoolVar(&install, "install", false, "replace the running binary after verification")
	return cmd
}

func installArtifact(path string) (string, error) {
	lower := strings.ToLower(path)
	temp, err := os.CreateTemp("", "clavis-upgrade-*")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempPath)
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		file, err := os.Open(path)
		if err != nil {
			cleanup()
			return "", err
		}
		defer file.Close()
		gz, err := gzip.NewReader(file)
		if err != nil {
			cleanup()
			return "", err
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		for {
			header, err := reader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				cleanup()
				return "", err
			}
			base := filepath.Base(header.Name)
			if header.Typeflag == tar.TypeReg && (base == "clavis" || base == "clavis.exe") {
				if _, err := io.Copy(temp, reader); err != nil {
					cleanup()
					return "", err
				}
				return tempPath, temp.Close()
			}
		}
		cleanup()
		return "", fmt.Errorf("archive does not contain a clavis binary")
	}
	if strings.HasSuffix(lower, ".zip") {
		archive, err := zip.OpenReader(path)
		if err != nil {
			cleanup()
			return "", err
		}
		defer archive.Close()
		for _, file := range archive.File {
			base := filepath.Base(file.Name)
			if base != "clavis" && base != "clavis.exe" {
				continue
			}
			reader, err := file.Open()
			if err != nil {
				cleanup()
				return "", err
			}
			_, copyErr := io.Copy(temp, reader)
			_ = reader.Close()
			if copyErr != nil {
				cleanup()
				return "", copyErr
			}
			return tempPath, temp.Close()
		}
		cleanup()
		return "", fmt.Errorf("archive does not contain a clavis binary")
	}
	file, err := os.Open(path)
	if err != nil {
		cleanup()
		return "", err
	}
	defer file.Close()
	if _, err := io.Copy(temp, file); err != nil {
		cleanup()
		return "", err
	}
	return tempPath, temp.Close()
}
