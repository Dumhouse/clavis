package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSchemaPullPushAndInfer(t *testing.T) {
	var pushed map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/projects/web-app/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": []map[string]any{
				{"key": "DATABASE_URL", "type": "url", "kind": "variable"},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/orgs/acme/projects/web-app/schema":
			if err := json.NewDecoder(r.Body).Decode(&pushed); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": []map[string]any{
				{"key": "DATABASE_URL", "type": "url", "kind": "variable"},
			}})
		case r.URL.Path == "/api/v1/orgs/acme/projects/web-app/environments/dev/secrets:resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{
				{"key": "DATABASE_URL", "value": "postgres://localhost/db", "kind": "variable"},
				{"key": "PORT", "value": "8080", "kind": "variable"},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("CLAVIS_TOKEN", "tok")

	newOpts := func() (*Options, *bytes.Buffer, *bytes.Buffer) {
		var out, errOut bytes.Buffer
		return &Options{
			ServerURL: server.URL, Org: "acme", Project: "web-app", Env: "dev",
			Output: NewTestOutput(&out, &errOut, false, false),
		}, &out, &errOut
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clavis.schema.yaml")
	opts, out, _ := newOpts()
	cmd := newSchemaPullCommand(opts)
	cmd.SetArgs([]string{"-o", path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "key: DATABASE_URL") || !strings.Contains(out.String(), "Wrote") {
		t.Fatalf("pull = %q / %q", raw, out.String())
	}

	opts, out, _ = newOpts()
	cmd = newSchemaPushCommand(opts)
	cmd.SetArgs([]string{path})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if pushed == nil || !strings.Contains(out.String(), "Pushed 1") {
		t.Fatalf("push body = %#v output=%q", pushed, out.String())
	}

	opts, out, _ = newOpts()
	cmd = newSchemaInferCommand(opts)
	cmd.SetArgs(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "key: PORT") || !strings.Contains(text, "type: int") || strings.Contains(text, "8080") {
		t.Fatalf("infer output = %q", text)
	}
}
