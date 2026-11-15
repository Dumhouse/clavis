package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/authn"
)

func TestScanBytesFindsTokensAndValues(t *testing.T) {
	token, err := authn.GenerateToken(authn.TokenMachine)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("first line\n" + token + "\nvalue=super-secret\nDUP super-secret\n")
	findings := scanBytes(data, "app.env", map[string]string{"DB_PASSWORD": "super-secret"})
	if len(findings) != 3 {
		t.Fatalf("findings = %#v", findings)
	}
	if findings[0].Type != "clavis-token" || findings[0].Line != 2 {
		t.Fatalf("token finding = %#v", findings[0])
	}
	if findings[1].Key != "DB_PASSWORD" || findings[1].Line != 3 || findings[2].Line != 4 {
		t.Fatalf("value findings = %#v", findings[1:])
	}
}

func TestScanBytesIgnoresInvalidToken(t *testing.T) {
	findings := scanBytes([]byte("clv_mt_abcdefghij\n"), "x", nil)
	if len(findings) != 0 {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestScanHistoryFindsDeletedSecrets(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	token, err := authn.GenerateToken(authn.TokenMachine)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		command := exec.Command("git", append([]string{"-c", "user.email=t@example.com", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...) //nolint:gosec // G204: test helper with fixed git arguments
		command.Dir = dir
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "app.env"), []byte("keep=1\nTOKEN="+token+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "oops")
	if err := os.WriteFile(filepath.Join(dir, "app.env"), []byte("keep=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "remove it")

	findings, commits, err := scanHistory(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if commits != 2 {
		t.Fatalf("commits = %d", commits)
	}
	if len(findings) != 1 || findings[0].File != "app.env" || findings[0].Line != 2 || len(findings[0].Commit) != 40 {
		t.Fatalf("findings = %+v", findings)
	}
	for _, finding := range findings {
		if strings.Contains(finding.Type+finding.Key+finding.File, token) {
			t.Fatal("token leaked into the finding")
		}
	}
}
