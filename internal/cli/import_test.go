package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseDotenv(t *testing.T) {
	input := "\uFEFF# comment\n\nDATABASE_URL=postgres://localhost/db\nexport API_KEY=\"abc\\n123\"\nSINGLE='a # b'\nEMPTY=\nINLINE=value # trailing\nHASH_NOT_COMMENT=abc#def\nESCAPES=\"quote: \\\" back: \\\\ tab:\\t\"\n"
	got, err := ParseDotenv([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"DATABASE_URL":     "postgres://localhost/db",
		"API_KEY":          "abc\n123",
		"SINGLE":           "a # b",
		"EMPTY":            "",
		"INLINE":           "value",
		"HASH_NOT_COMMENT": "abc#def",
		"ESCAPES":          "quote: \" back: \\ tab:\t",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func TestParseDotenvErrors(t *testing.T) {
	cases := []string{
		"NO_EQUALS\n",
		"=value\n",
		`A="unterminated` + "\n",
		"A='unterminated\n",
		`A="trailing\` + "\n",
	}
	for _, input := range cases {
		if _, err := ParseDotenv([]byte(input)); err == nil {
			t.Errorf("accepted %q", input)
		}
	}
}

func TestParseImportFile(t *testing.T) {
	jsonValues, err := parseImportFile("x.json", []byte(`{"A":"one","B":"two"}`))
	if err != nil || jsonValues["A"] != "one" {
		t.Fatalf("json: %v %v", jsonValues, err)
	}
	yamlValues, err := parseImportFile("x.yaml", []byte("A: one\nB: two\n"))
	if err != nil || yamlValues["B"] != "two" {
		t.Fatalf("yaml: %v %v", yamlValues, err)
	}
	dotenvValues, err := parseImportFile("x.env", []byte("A=one\n"))
	if err != nil || dotenvValues["A"] != "one" {
		t.Fatalf("dotenv: %v %v", dotenvValues, err)
	}
	if _, err := parseImportFile("x.json", []byte("nope")); err == nil {
		t.Fatal("bad JSON accepted")
	}
	if _, err := parseImportFile("x.yaml", []byte(":\n  - no")); err == nil {
		t.Fatal("bad YAML accepted")
	}
}

func FuzzParseDotenv(f *testing.F) {
	seeds := []string{
		"",
		"A=1\n",
		`A="x"`,
		"export A='y'\n# c\n",
		"A=a b c # d\n",
		"A=\"\\n\\t\"\n",
		"=bad\n",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = ParseDotenv(data)
	})
}

func TestProviderImportDryRunAndMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v3/secrets/raw":
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]string{
				{"secretKey": "SOURCE_KEY", "secretValue": "new-value"},
				{"secretKey": "DROP_KEY", "secretValue": "x"},
			}})
		case strings.HasSuffix(r.URL.Path, "/secrets:resolve"):
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
				{"key": "TARGET_KEY", "value": "old-value", "kind": "secret"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("CLAVIS_TOKEN", "tok")

	dir := t.TempDir()
	mapPath := filepath.Join(dir, "map.yaml")
	if err := os.WriteFile(mapPath, []byte("keys:\n  SOURCE_KEY: TARGET_KEY\n  DROP_KEY: \"\"\nfolders:\n  \"\": /imported\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	opts := &Options{
		JSON: true, ServerURL: server.URL, Org: "acme", Project: "web-app", Env: "dev",
		Output: NewTestOutput(&out, &errOut, true, false),
	}
	cmd := newImportCommand(opts)
	cmd.SetArgs([]string{
		"--provider", "infisical", "--base-url", server.URL, "--token", "inf-token",
		"--remote-project", "ws", "--remote-env", "dev", "--map", mapPath, "--dry-run",
	})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatalf("decode dry run: %v (%s)", err, out.String())
	}
	if len(rows) != 1 || rows[0]["key"] != "TARGET_KEY" || rows[0]["action"] != "changed" || rows[0]["folder"] != "/imported" {
		t.Fatalf("rows = %#v", rows)
	}
	if strings.Contains(out.String(), "new-value") || strings.Contains(out.String(), "old-value") {
		t.Fatalf("dry run leaked a value: %s", out.String())
	}
}
