package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestUpgradeVerifiesWithCosign(t *testing.T) {
	dir := t.TempDir()
	cosign := filepath.Join(dir, "cosign")
	if err := os.WriteFile(cosign, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	artifact := filepath.Join(dir, "clavis.tar.gz")
	if err := os.WriteFile(artifact, []byte("binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(dir, "bundle.json")
	if err := os.WriteFile(bundle, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &errOut, false, false)}
	cmd := newUpgradeCommand(opts)
	cmd.SetArgs([]string{artifact, "--bundle", bundle})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("Verified")) {
		t.Fatalf("output = %q", out.String())
	}
}

func TestInstallArtifactExtractsBinary(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "clavis.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	content := []byte("binary")
	if err := tw.WriteHeader(&tar.Header{Name: "clavis", Mode: 0o755, Size: int64(len(content))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gz.Close()
	_ = file.Close()
	path, err := installArtifact(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "binary" {
		t.Fatalf("artifact = %q %v", data, err)
	}
}
