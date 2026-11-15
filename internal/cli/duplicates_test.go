package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func sec(key, value string) secretJSON { return secretJSON{Key: key, Kind: "secret", Value: value} }

func TestFindDuplicates(t *testing.T) {
	envs := []envSecrets{
		{Env: "dev", Type: "development", Secrets: []secretJSON{
			sec("STRIPE_KEY", "sk-same-value-123"),
			sec("STRIPE_BACKUP", "sk-same-value-123"),
			sec("PORT", "3000"),
			{Key: "FLAG", Kind: "variable", Value: "enabled-everywhere"},
			{Key: "INHERITED", Kind: "secret", Value: "inherited-value-9", Inherited: true},
		}},
		{Env: "production", Type: "production", Secrets: []secretJSON{
			sec("STRIPE_KEY", "sk-same-value-123"),
			sec("DB_PASSWORD", "different-in-prod"),
			sec("PORT", "3000"),
			{Key: "FLAG", Kind: "variable", Value: "enabled-everywhere"},
			{Key: "INHERITED", Kind: "secret", Value: "inherited-value-9"},
		}},
		{Env: "staging", Type: "staging", Secrets: []secretJSON{
			sec("DB_PASSWORD", "staging-own-value"),
		}},
	}
	report := findDuplicates(envs)
	if len(report.SameValue) != 1 || report.SameValue[0].Env != "dev" ||
		strings.Join(report.SameValue[0].Keys, ",") != "STRIPE_BACKUP,STRIPE_KEY" {
		t.Fatalf("same value: %+v", report.SameValue)
	}
	if len(report.SharedKey) != 1 || report.SharedKey[0].Key != "STRIPE_KEY" || !report.SharedKey[0].WithProduction ||
		strings.Join(report.SharedKey[0].Environments, ",") != "dev,production" {
		t.Fatalf("shared key: %+v", report.SharedKey)
	}
}

func TestFindDuplicatesNeverReturnsValues(t *testing.T) {
	report := findDuplicates([]envSecrets{
		{Env: "a", Secrets: []secretJSON{sec("K1", "super-secret-value"), sec("K2", "super-secret-value")}},
	})
	if strings.Contains(strings.Join(report.SameValue[0].Keys, " "), "super-secret") {
		t.Fatal("a value leaked into the report")
	}
}

func TestSimilarEnvironments(t *testing.T) {
	envs := []envSecrets{
		{Env: "dev", Secrets: []secretJSON{sec("A", "1111111"), sec("B", "2222222"), sec("C", "3333333")}},
		{Env: "staging", Secrets: []secretJSON{sec("A", "1111111"), sec("B", "different"), sec("D", "4444444")}},
		{Env: "prod", Secrets: []secretJSON{sec("Z", "9999999")}},
	}
	pairs := similarEnvironments(envs)
	if len(pairs) != 3 {
		t.Fatalf("pairs = %d", len(pairs))
	}
	first := pairs[0]
	if first.A != "dev" || first.B != "staging" || first.CommonKeys != 2 || first.SameValues != 1 ||
		first.ValuePercent != 50 || first.KeyPercent != 50 || first.OnlyA != 1 || first.OnlyB != 1 {
		t.Fatalf("dev/staging: %+v", first)
	}
}

func TestEnvRemoveRequiresConfirmationAndDeletes(t *testing.T) {
	deleted := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/orgs/acme/projects/web":
			_, _ = w.Write([]byte(`{"slug":"web","environments":[{"slug":"dev","type":"development"},{"slug":"qa","type":"staging"}]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/environments/qa/secrets"):
			_, _ = w.Write([]byte(`{"secrets":[{"key":"A"},{"key":"B"}]}`))
		case r.Method == http.MethodDelete:
			deleted = r.URL.Path
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	doctorEnv(t, srv.URL, "good", "dev")

	run := func(args ...string) error {
		root := NewRootCommand()
		root.SetArgs(append([]string{"env", "rm"}, args...))
		return root.Execute()
	}
	// No terminal in tests: without --yes it must refuse and delete nothing.
	if err := run("qa"); err == nil || deleted != "" {
		t.Fatalf("expected a refusal, err=%v deleted=%q", err, deleted)
	}
	if err := run("nope", "--yes"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unknown environment: %v", err)
	}
	if err := run("qa", "--yes"); err != nil {
		t.Fatal(err)
	}
	if deleted != "/api/v1/orgs/acme/projects/web/environments/qa" {
		t.Fatalf("deleted %q", deleted)
	}
}
