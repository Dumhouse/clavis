package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckEnvironmentReportsRequiredAndTypes(t *testing.T) {
	schema := []schemaJSON{
		{
			Key: "DATABASE_URL", Type: "url", Kind: "variable",
			Required: map[string]bool{"production": true}, Pattern: "^postgres://",
		},
		{Key: "LOG_LEVEL", Type: "enum", Kind: "variable", Enum: []string{"info", "warn"}},
		{Key: "MISSING_OPTIONAL", Type: "string"},
	}
	secrets := []secretJSON{
		{Key: "DATABASE_URL", Value: "mysql://localhost/db", Kind: "variable"},
		{Key: "LOG_LEVEL", Value: "verbose", Kind: "variable"},
		{Key: "EXTRA_KEY", Value: "x", Kind: "variable"},
	}
	result := checkEnvironment(schema, secrets, environmentJSON{Slug: "prod", Type: "production"})
	if result.OK {
		t.Fatal("expected failed check")
	}
	statuses := map[string]string{}
	for _, item := range result.Checks {
		statuses[item.Key] = item.Status
	}
	if statuses["DATABASE_URL"] != "error" || statuses["LOG_LEVEL"] != "error" {
		t.Fatalf("statuses = %#v", statuses)
	}
	if statuses["MISSING_OPTIONAL"] != "warning" || statuses["EXTRA_KEY"] != "warning" {
		t.Fatalf("warnings = %#v", statuses)
	}
}

func TestCheckEnvironmentPasses(t *testing.T) {
	schema := []schemaJSON{
		{Key: "DATABASE_URL", Type: "url", Kind: "variable", Required: map[string]bool{"production": true}},
		{Key: "ENABLED", Type: "bool", Kind: "variable"},
	}
	secrets := []secretJSON{
		{Key: "DATABASE_URL", Value: "postgres://localhost/db", Kind: "variable"},
		{Key: "ENABLED", Value: "true", Kind: "variable"},
	}
	result := checkEnvironment(schema, secrets, environmentJSON{Slug: "prod", Type: "production"})
	if !result.OK {
		t.Fatalf("unexpected failures: %#v", result.Checks)
	}
	for _, item := range result.Checks {
		if item.Status != "ok" {
			t.Fatalf("status = %#v", result.Checks)
		}
	}
}

func TestCheckCommandExitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/orgs/acme/projects/web-app/schema":
			_ = json.NewEncoder(w).Encode(map[string]any{"schema": []map[string]any{
				{"key": "REQUIRED_KEY", "type": "string", "required": map[string]bool{"production": true}},
			}})
		case "/api/v1/orgs/acme/projects/web-app":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"slug": "web-app", "environments": []map[string]string{{"slug": "prod", "type": "production"}},
			})
		case "/api/v1/orgs/acme/projects/web-app/environments/prod/secrets:resolve":
			_ = json.NewEncoder(w).Encode(map[string]any{"secrets": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	t.Setenv("CLAVIS_TOKEN", "tok")

	var out, errOut bytes.Buffer
	opts := &Options{
		ServerURL: server.URL, Org: "acme", Project: "web-app", Env: "prod",
		Output: NewTestOutput(&out, &errOut, false, false),
	}
	cmd := newCheckCommand(opts)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected check failure")
	}
	if code := ExitCode(err); code != ExitValidation {
		t.Fatalf("exit code = %d, want %d (err=%v)", code, ExitValidation, err)
	}
	if !strings.Contains(out.String(), "required in production") {
		t.Fatalf("report = %q", out.String())
	}
}

func TestInferType(t *testing.T) {
	cases := map[string]string{
		"true":                  "bool",
		"42":                    "int",
		`{"a":1}`:               "json",
		"https://example.com/x": "url",
		"postgres://db/app":     "url",
		"hello":                 "string",
		"":                      "string",
	}
	for value, want := range cases {
		if got := inferType(value); got != want {
			t.Errorf("inferType(%q) = %q, want %q", value, got, want)
		}
	}
	if got := inferSchema([]secretJSON{{Key: "B", Value: "1", Kind: "variable"}, {Key: "A", Value: "x"}}); len(got) != 2 || got[0].Key != "A" || got[0].Kind != "secret" {
		t.Fatalf("inferSchema = %#v", got)
	}
}
