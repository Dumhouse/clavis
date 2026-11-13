package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupTokenCommand(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAVIS_DATA_DIR", dir)
	t.Setenv("CLAVIS_DATABASE_PATH", "")

	cmd := newSetupTokenCommand(&Options{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "no setup token") {
		t.Fatalf("missing file: err = %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "setup-token"), []byte("abc123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := newSetupTokenCmdForTest(&out).Execute(); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "abc123" {
		t.Fatalf("output = %q", got)
	}
}

func newSetupTokenCmdForTest(out *bytes.Buffer) interface{ Execute() error } {
	cmd := newSetupTokenCommand(&Options{})
	cmd.SetOut(out)
	cmd.SetArgs(nil)
	return cmd
}
