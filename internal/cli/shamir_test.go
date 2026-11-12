package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dumhouse/clavis/internal/crypto"
)

func TestShamirSealAndUnseal(t *testing.T) {
	dir := t.TempDir()
	rootPath := filepath.Join(dir, "root.key")
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := crypto.NewFileProvider(rootPath, false).Save(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "clavis.yaml")
	raw := "database:\n  driver: sqlite\n  path: " + filepath.Join(dir, "clavis.db") + "\n" +
		"keys:\n  provider: file\n  root_key_file: " + rootPath + "\n"
	if err := os.WriteFile(configPath, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &errOut, false, false)}
	split := newAdminShamirSplitCommand(opts)
	split.SetArgs([]string{"--config", configPath, "--out", dir, "--shares", "3", "--threshold", "2", "--seal", "--yes"})
	if err := split.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Fatal("root key still exists after sealing")
	}
	if _, err := os.Stat(rootPath + ".sealed"); err != nil {
		t.Fatal("sealed marker missing")
	}
	unseal := newAdminUnsealCommand(opts)
	unseal.SetArgs([]string{
		"--config", configPath,
		"--share", filepath.Join(dir, "root-share-01.key"),
		"--share", filepath.Join(dir, "root-share-03.key"),
	})
	if err := unseal.Execute(); err != nil {
		t.Fatal(err)
	}
	recovered, err := crypto.NewFileProvider(rootPath, false).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recovered, key) {
		t.Fatal("unsealed key differs")
	}
}
