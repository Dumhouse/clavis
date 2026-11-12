package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSecretCacheRoundTripAndTamper(t *testing.T) {
	t.Setenv("CLAVIS_CREDENTIAL_FILE", filepath.Join(t.TempDir(), "creds.json"))
	cacheDir := t.TempDir()
	t.Setenv("CLAVIS_CACHE_DIR", cacheDir)
	resolved := &resolvedContext{Server: "https://clavis.example", Org: "acme", Project: "web", Env: "dev"}
	secrets := []secretJSON{{Key: "DATABASE_URL", Value: "postgres://secret", Kind: "variable"}}
	if err := saveSecretCache(resolved, secrets); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSecretCache(resolved)
	if err != nil || len(loaded) != 1 || loaded[0].Value != "postgres://secret" {
		t.Fatalf("load = %#v %v", loaded, err)
	}
	path, err := cachePath(resolved)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xff
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecretCache(resolved); err == nil {
		t.Fatal("tampered cache decrypted")
	}
}

func TestSecretCacheScopeIsolation(t *testing.T) {
	t.Setenv("CLAVIS_CREDENTIAL_FILE", filepath.Join(t.TempDir(), "creds.json"))
	t.Setenv("CLAVIS_CACHE_DIR", t.TempDir())
	dev := &resolvedContext{Server: "https://clavis.example", Org: "acme", Project: "web", Env: "dev"}
	prod := &resolvedContext{Server: "https://clavis.example", Org: "acme", Project: "web", Env: "prod"}
	if err := saveSecretCache(dev, []secretJSON{{Key: "KEY", Value: "dev"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSecretCache(prod); err == nil {
		t.Fatal("prod loaded dev cache")
	}
}
