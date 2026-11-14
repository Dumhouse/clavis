package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/updatecheck"
	"github.com/dumhouse/clavis/internal/version"
)

// fakeRelease serves an archive holding a "clavis" binary with the given body
// and a checksums.txt, like a GitHub release.
func fakeRelease(t *testing.T, target, body string, corrupt bool) *httptest.Server {
	t.Helper()
	ext := ".tar.gz"
	var archive bytes.Buffer
	if runtime.GOOS == "windows" {
		ext = ".zip"
		zw := zip.NewWriter(&archive)
		w, _ := zw.Create("clavis.exe")
		_, _ = w.Write([]byte(body))
		_ = zw.Close()
	} else {
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "clavis", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(body))
		_ = tw.Close()
		_ = gz.Close()
	}
	name := "clavis_" + target + "_" + runtime.GOOS + "_" + runtime.GOARCH + ext
	sum := sha256.Sum256(archive.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if corrupt {
		hexSum = strings.Repeat("0", 64)
	}
	sums := hexSum + "  " + name + "\n"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v" + target + "/" + name:
			_, _ = w.Write(archive.Bytes())
		case "/v" + target + "/checksums.txt":
			_, _ = w.Write([]byte(sums))
		default:
			http.NotFound(w, r)
		}
	}))
}

func stubUpgrade(t *testing.T, method, command, latest string) string {
	t.Helper()
	t.Setenv("PATH", t.TempDir()) // no cosign
	current := filepath.Join(t.TempDir(), "clavis")
	if err := os.WriteFile(current, []byte("old"), 0o755); err != nil { //nolint:gosec // G306: a test binary
		t.Fatal(err)
	}
	restoreExe, restoreInstall, restoreLatest := currentExecutable, currentInstall, fetchLatestRelease
	currentExecutable = func() (string, error) { return current, nil }
	currentInstall = func(string) updatecheck.Install {
		return updatecheck.Install{Method: method, Label: method, Command: command}
	}
	fetchLatestRelease = func(context.Context) (string, error) { return latest, nil }
	oldVersion := version.Version
	version.Version = "0.3.2"
	t.Cleanup(func() {
		currentExecutable, currentInstall, fetchLatestRelease = restoreExe, restoreInstall, restoreLatest
		version.Version = oldVersion
	})
	return current
}

func TestUpgradeReplacesDownloadedBinary(t *testing.T) {
	current := stubUpgrade(t, "script", "", "0.3.3")
	release := fakeRelease(t, "0.3.3", "new binary", false)
	defer release.Close()
	var out, errOut bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &errOut, false, false)}
	if err := upgradeToLatest(context.Background(), opts, "", release.URL, true); err != nil {
		t.Fatalf("upgrade: %v\n%s%s", err, out.String(), errOut.String())
	}
	if got, _ := os.ReadFile(current); string(got) != "new binary" { //nolint:gosec // G304: test file
		t.Fatalf("binary = %q", got)
	}
	if !strings.Contains(out.String(), "Checksum verified") || !strings.Contains(out.String(), "from 0.3.2 to 0.3.3") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestUpgradeRejectsBadChecksum(t *testing.T) {
	current := stubUpgrade(t, "binary", "", "0.3.3")
	release := fakeRelease(t, "0.3.3", "tampered", true)
	defer release.Close()
	opts := &Options{Output: NewTestOutput(&bytes.Buffer{}, &bytes.Buffer{}, false, false)}
	err := upgradeToLatest(context.Background(), opts, "", release.URL, true)
	if err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := os.ReadFile(current); string(got) != "old" { //nolint:gosec // G304: test file
		t.Fatalf("binary changed to %q", got)
	}
}

func TestUpgradeRunsPackageManager(t *testing.T) {
	stubUpgrade(t, "homebrew", "brew upgrade dumhouse/tap/clavis", "0.3.3")
	var ran string
	restore := runUpgradeCommand
	runUpgradeCommand = func(_ context.Context, _ *Output, command string) error {
		ran = command
		return nil
	}
	t.Cleanup(func() { runUpgradeCommand = restore })
	opts := &Options{Output: NewTestOutput(&bytes.Buffer{}, &bytes.Buffer{}, false, false)}
	if err := upgradeToLatest(context.Background(), opts, "", "", true); err != nil {
		t.Fatal(err)
	}
	if ran != "brew upgrade dumhouse/tap/clavis" {
		t.Fatalf("ran %q", ran)
	}
}

func TestUpgradeExplainsServerInstalls(t *testing.T) {
	stubUpgrade(t, "docker", "docker compose pull && docker compose up -d", "0.3.3")
	var out bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &bytes.Buffer{}, false, false)}
	if err := upgradeToLatest(context.Background(), opts, "", "", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "docker compose pull") {
		t.Fatalf("output = %s", out.String())
	}
}

func TestUpgradeNothingToDo(t *testing.T) {
	stubUpgrade(t, "homebrew", "brew upgrade dumhouse/tap/clavis", "0.3.2")
	var out bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &bytes.Buffer{}, false, false)}
	if err := upgradeToLatest(context.Background(), opts, "", "", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing to do") {
		t.Fatalf("output = %s", out.String())
	}
}
