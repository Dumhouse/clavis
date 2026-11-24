package importers

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseBitwardenSecrets(t *testing.T) {
	raw := []byte(`[
		{"id":"1","projectId":"p","key":"DATABASE_URL","value":"postgres://db"},
		{"id":"2","projectId":"p","key":"EMPTY","value":""},
		{"id":"3","projectId":"p","key":"  ","value":"ignored"},
		{"id":"4","projectId":"p","key":"API_TOKEN","value":"abc","note":"prod","creationDate":"2026-01-01T00:00:00Z"}
	]`)
	entries, err := parseBitwardenSecrets(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	got := map[string]string{}
	for _, entry := range entries {
		if entry.Kind != "secret" || entry.Source != "bitwarden" {
			t.Fatalf("entry = %#v", entry)
		}
		got[entry.Key] = entry.Value
	}
	want := map[string]string{"DATABASE_URL": "postgres://db", "EMPTY": "", "API_TOKEN": "abc"}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("got %#v, want %#v", got, want)
		}
	}
}

func TestParseBitwardenSecretsRejectsBadJSON(t *testing.T) {
	if _, err := parseBitwardenSecrets([]byte("not json")); err == nil {
		t.Fatal("bad JSON accepted")
	}
}

func TestLoadBitwardenRequiresProject(t *testing.T) {
	_, err := Load(context.Background(), Options{Provider: "bitwarden"})
	if err == nil || !strings.Contains(err.Error(), "--remote-project") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBitwardenMissingCLI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := Load(context.Background(), Options{Provider: "bitwarden", RemoteProject: "project-1"})
	if err == nil || !strings.Contains(err.Error(), "bws") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadBitwardenRunsCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake bws CLI is a shell script")
	}
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	tokenFile := filepath.Join(dir, "token")
	script := `#!/bin/sh
printf '%s\n' "$@" > "$BWS_TEST_ARGS"
printf '%s' "${BWS_ACCESS_TOKEN:-}" > "$BWS_TEST_TOKEN"
printf '%s' '[{"key":"API_TOKEN","value":"abc","note":"prod"},{"key":"DATABASE_URL","value":"postgres://db"}]'
`
	if err := os.WriteFile(filepath.Join(dir, "bws"), []byte(script), 0o700); err != nil { //nolint:gosec // G306: executable test fixture
		t.Fatal(err)
	}
	t.Setenv("BWS_TEST_ARGS", argsFile)
	t.Setenv("BWS_TEST_TOKEN", tokenFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BWS_ACCESS_TOKEN", "inherited-token")

	entries, err := Load(context.Background(), Options{
		Provider: "bitwarden", RemoteProject: "project-1", Token: "flag-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Key != "API_TOKEN" || entries[1].Key != "DATABASE_URL" {
		t.Fatalf("entries = %#v", entries)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Fields(string(args)); len(got) != 5 ||
		got[0] != "secret" || got[1] != "list" || got[2] != "project-1" || got[3] != "--output" || got[4] != "json" {
		t.Fatalf("args = %q", got)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(token) != "flag-token" {
		t.Fatalf("BWS_ACCESS_TOKEN = %q", token)
	}
}

func TestLoadBitwardenKeepsInheritedToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake bws CLI is a shell script")
	}
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	script := `#!/bin/sh
printf '%s' "${BWS_ACCESS_TOKEN:-}" > "$BWS_TEST_TOKEN"
printf '%s' '[]'
`
	if err := os.WriteFile(filepath.Join(dir, "bws"), []byte(script), 0o700); err != nil { //nolint:gosec // G306: executable test fixture
		t.Fatal(err)
	}
	t.Setenv("BWS_TEST_TOKEN", tokenFile)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BWS_ACCESS_TOKEN", "inherited-token")

	if _, err := Load(context.Background(), Options{Provider: "bitwarden", RemoteProject: "project-1"}); err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(token) != "inherited-token" {
		t.Fatalf("BWS_ACCESS_TOKEN = %q", token)
	}
}

func TestLoadBitwardenReportsCLIErrors(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake bws CLI is a shell script")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
printf '%s' 'not logged in' >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(dir, "bws"), []byte(script), 0o700); err != nil { //nolint:gosec // G306: executable test fixture
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	_, err := Load(context.Background(), Options{Provider: "bitwarden", RemoteProject: "project-1"})
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("err = %v", err)
	}
}
