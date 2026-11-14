package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dumhouse/clavis/internal/updatecheck"
	"github.com/dumhouse/clavis/internal/version"
)

// Seams for tests.
var (
	currentExecutable = func() (string, error) {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(exe)
	}
	runUpgradeCommand = func(ctx context.Context, out *Output, command string) error {
		shell, flag := "sh", "-c"
		if runtime.GOOS == "windows" {
			shell, flag = "cmd", "/C"
		}
		c := exec.CommandContext(ctx, shell, flag, command) //nolint:gosec // G204: a fixed command from updatecheck.Detect
		c.Stdin, c.Stdout, c.Stderr = os.Stdin, out.Out, out.Err
		return c.Run()
	}
	downloadHTTPClient = &http.Client{Timeout: 5 * time.Minute}
)

// selfUpgrade lists install methods `clavis upgrade` can finish by itself,
// either by running the package manager or by replacing the binary.
var selfUpgrade = map[string]bool{
	"homebrew": true, "scoop": true, "winget": true, "go-install": true, "script": true, "binary": true,
}

func newUpgradeCommand(opts *Options) *cobra.Command {
	var bundle, identity, issuer, target, downloadBase string
	var install, yes bool
	cmd := &cobra.Command{
		Use:   "upgrade [ARCHIVE]",
		Short: "Install the latest Clavis release",
		Long: "Upgrade this copy of Clavis to the latest release, the way it was installed.\n" +
			"With ARCHIVE, verify a downloaded release with cosign (and install it with --install).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				return upgradeFromArchive(cmd, opts, args[0], bundle, identity, issuer, install)
			}
			return upgradeToLatest(cmd.Context(), opts, target, downloadBase, yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "don't ask before upgrading")
	cmd.Flags().StringVar(&target, "version", "", "version to install (default: the latest release)")
	cmd.Flags().StringVar(&downloadBase, "download-base", "", "where releases are downloaded from, for a mirror (default: GitHub releases)")
	cmd.Flags().StringVar(&bundle, "bundle", "", "with ARCHIVE: Sigstore bundle (default ARCHIVE.sigstore.json)")
	cmd.Flags().StringVar(&identity, "identity", "", "with ARCHIVE: certificate identity regexp")
	cmd.Flags().StringVar(&issuer, "issuer", "", "with ARCHIVE: OIDC issuer")
	cmd.Flags().BoolVar(&install, "install", false, "with ARCHIVE: replace the running binary after verification")
	return cmd
}

func upgradeToLatest(ctx context.Context, opts *Options, target, downloadBase string, yes bool) error {
	out := opts.output()
	if target == "" {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		latest, err := fetchLatestRelease(ctx)
		if err != nil {
			return fmt.Errorf("could not check for a new release: %w", err)
		}
		target = latest
	}
	target = strings.TrimPrefix(target, "v")
	install := currentInstall(target)
	if !updatecheck.Newer(target, version.Version) && install.Method != "source" {
		out.Success("Clavis %s is the latest release; nothing to do.", version.Version)
		return nil
	}
	if !selfUpgrade[install.Method] {
		out.Info("This copy was installed with %s, which clavis can't upgrade by itself.", install.Label)
		if install.Command != "" {
			out.Info("Upgrade it with:")
			out.Info("  %s", install.Command)
		}
		if install.Note != "" {
			out.Info("%s", install.Note)
		}
		return nil
	}
	if install.Method == "script" || install.Method == "binary" {
		return replaceWithRelease(ctx, opts, target, downloadBase, yes)
	}
	if install.Command == "" {
		return fmt.Errorf("no upgrade command for %s", install.Label)
	}
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // G115: file descriptors fit in int
			return fmt.Errorf("pass --yes to run: %s", install.Command)
		}
		ok, err := confirm(fmt.Sprintf("Upgrade %s → %s with %q?", version.Version, target, install.Command))
		if err != nil || !ok {
			return err
		}
	}
	out.Info("Running: %s", install.Command)
	if err := runUpgradeCommand(ctx, out, install.Command); err != nil {
		return fmt.Errorf("%s failed: %w", install.Label, err)
	}
	out.Success("Upgraded with %s.", install.Label)
	return nil
}

// replaceWithRelease downloads the release archive for this system, checks it
// against the release's checksums.txt (and its cosign signature when cosign
// is installed), and swaps it in for the running binary.
func replaceWithRelease(ctx context.Context, opts *Options, target, downloadBase string, yes bool) error {
	out := opts.output()
	current, err := currentExecutable()
	if err != nil {
		return err
	}
	if !yes {
		if !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // G115: file descriptors fit in int
			return fmt.Errorf("pass --yes to replace %s with Clavis %s", current, target)
		}
		ok, err := confirm(fmt.Sprintf("Replace %s (%s) with Clavis %s?", current, version.Version, target))
		if err != nil || !ok {
			return err
		}
	}
	if downloadBase == "" {
		downloadBase = firstNonEmpty(os.Getenv("CLAVIS_DOWNLOAD_BASE"), "https://github.com/Dumhouse/clavis/releases/download")
	}
	base := strings.TrimRight(downloadBase, "/") + "/v" + target + "/"
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	name := fmt.Sprintf("clavis_%s_%s_%s%s", target, runtime.GOOS, runtime.GOARCH, ext)
	dir, err := os.MkdirTemp("", "clavis-upgrade-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	out.Info("Downloading %s…", name)
	archive := filepath.Join(dir, name)
	if err := downloadFile(ctx, base+name, archive); err != nil {
		return err
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := downloadFile(ctx, base+"checksums.txt", sums); err != nil {
		return err
	}
	if err := verifyChecksum(archive, sums, name); err != nil {
		return err
	}
	if cosign, err := exec.LookPath("cosign"); err == nil {
		bundle := sums + ".sigstore.json"
		if err := downloadFile(ctx, base+"checksums.txt.sigstore.json", bundle); err != nil {
			return err
		}
		verify := exec.CommandContext(ctx, cosign, "verify-blob", "--bundle", bundle, //nolint:gosec // G204: fixed arguments
			"--certificate-identity-regexp", "https://github.com/dumhouse/clavis/.*",
			"--certificate-oidc-issuer", "https://token.actions.githubusercontent.com", sums)
		verify.Stdout, verify.Stderr = io.Discard, out.Err
		if err := verify.Run(); err != nil {
			return fmt.Errorf("release signature verification failed: %w", err)
		}
		out.Success("Checksum and signature verified.")
	} else {
		out.Success("Checksum verified. Install cosign to also verify the signature.")
	}
	binary, err := installArtifact(archive)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(binary) }()
	if err := swapBinary(binary, current); err != nil {
		return fmt.Errorf("could not replace %s: %w (try again with sudo, or reinstall with the install script)", current, err)
	}
	out.Success("Upgraded %s from %s to %s.", current, version.Version, target)
	return nil
}

func downloadFile(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clavis-cli/"+version.Version)
	resp, err := downloadHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	file, err := os.Create(path) //nolint:gosec // G304: a file in our own temp directory
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, io.LimitReader(resp.Body, maxUpgradeBinaryBytes)); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

// verifyChecksum checks path against its line in a goreleaser checksums.txt.
func verifyChecksum(path, sumsPath, name string) error {
	sums, err := os.ReadFile(sumsPath) //nolint:gosec // G304: a file in our own temp directory
	if err != nil {
		return err
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			want = strings.ToLower(fields[0])
		}
	}
	if want == "" {
		return fmt.Errorf("checksums.txt has no entry for %s", name)
	}
	file, err := os.Open(path) //nolint:gosec // G304: a file in our own temp directory
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hash.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}
	return nil
}

// swapBinary puts the new binary where the running one is. The old file is
// renamed first, which also works on Windows while it is running.
func swapBinary(newPath, current string) error {
	if err := os.Chmod(newPath, 0o755); err != nil { //nolint:gosec // G302: the binary must be executable
		return err
	}
	old := current + ".old"
	_ = os.Remove(old)
	if err := os.Rename(current, old); err != nil {
		return err
	}
	if err := moveFile(newPath, current); err != nil {
		_ = os.Rename(old, current)
		return err
	}
	if runtime.GOOS != "windows" {
		_ = os.Remove(old)
	}
	return nil
}

// moveFile renames, falling back to a copy when the temp directory is on
// another filesystem.
func moveFile(from, to string) error {
	if err := os.Rename(from, to); err == nil {
		return nil
	}
	src, err := os.Open(from) //nolint:gosec // G304: our own temp file
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	dst, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // G302,G304: the binary must be executable
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return err
	}
	return dst.Close()
}

// upgradeFromArchive is the original flow: verify a downloaded archive with
// cosign, and install it with --install.
func upgradeFromArchive(cmd *cobra.Command, opts *Options, archive, bundle, identity, issuer string, install bool) error {
	out := opts.output()
	if bundle == "" {
		bundle = archive + ".sigstore.json"
	}
	if identity == "" {
		identity = "https://github.com/dumhouse/clavis/.*"
	}
	if issuer == "" {
		issuer = "https://token.actions.githubusercontent.com"
	}
	cosign, err := exec.LookPath("cosign")
	if err != nil {
		return fmt.Errorf("cosign is required to verify a downloaded archive: %w", err)
	}
	verify := exec.CommandContext(cmd.Context(), cosign, "verify-blob", //nolint:gosec // G204: arguments are the user's own
		"--bundle", bundle,
		"--certificate-identity-regexp", identity,
		"--certificate-oidc-issuer", issuer,
		archive,
	)
	verify.Stdout = out.Out
	verify.Stderr = out.Err
	if err := verify.Run(); err != nil {
		return fmt.Errorf("release signature verification failed: %w", err)
	}
	if !install {
		out.Success("Verified %s. Re-run with --install to replace the current binary.", archive)
		return nil
	}
	current, err := currentExecutable()
	if err != nil {
		return err
	}
	temp, err := installArtifact(archive)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temp) }()
	if err := swapBinary(temp, current); err != nil {
		return err
	}
	out.Success("Installed verified binary at %s.", current)
	return nil
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
		defer func() { _ = file.Close() }()
		gz, err := gzip.NewReader(file)
		if err != nil {
			cleanup()
			return "", err
		}
		defer func() { _ = gz.Close() }()
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
				if _, err := io.Copy(temp, io.LimitReader(reader, maxUpgradeBinaryBytes)); err != nil {
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
		defer func() { _ = archive.Close() }()
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
			_, copyErr := io.Copy(temp, io.LimitReader(reader, maxUpgradeBinaryBytes))
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
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(temp, file); err != nil {
		cleanup()
		return "", err
	}
	return tempPath, temp.Close()
}

// maxUpgradeBinaryBytes caps how much a release archive may expand to, so a
// corrupt or hostile archive cannot fill the disk.
const maxUpgradeBinaryBytes = 1 << 30
