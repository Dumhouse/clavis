package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/updatecheck"
)

// fakeClavis answers the endpoints doctor uses. A request with a token
// other than "good" is refused.
func fakeClavis(t *testing.T, serverVersion string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(map[string]string{"version": serverVersion})
		case r.Header.Get("Authorization") != "Bearer good":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
		case r.URL.Path == "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"user":{"email":"ada@example.com","name":"Ada"},"orgs":[]}`))
		case r.URL.Path == "/api/v1/orgs/acme/projects/web":
			_, _ = w.Write([]byte(`{"slug":"web","environments":[{"slug":"dev"},{"slug":"prod"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func doctorEnv(t *testing.T, server, token, env string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Chdir(t.TempDir())
	t.Setenv("CLAVIS_SERVER", server)
	t.Setenv("CLAVIS_TOKEN", token)
	t.Setenv("CLAVIS_ORG", "acme")
	t.Setenv("CLAVIS_PROJECT", "web")
	t.Setenv("CLAVIS_ENV", env)
	restoreLatest, restoreInstall := fetchLatestRelease, currentInstall
	fetchLatestRelease = func(context.Context) (string, error) { return "0.3.2", nil }
	currentInstall = func(string) updatecheck.Install {
		return updatecheck.Install{Method: "homebrew", Label: "Homebrew", Command: "brew upgrade dumhouse/tap/clavis"}
	}
	t.Cleanup(func() { fetchLatestRelease, currentInstall = restoreLatest, restoreInstall })
}

func statusOf(checks []doctorCheck, name string) doctorStatus {
	for _, check := range checks {
		if check.Name == name {
			return check.Status
		}
	}
	return ""
}

func TestDoctorAllGood(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "good", "dev")
	checks := runDoctor(context.Background(), &Options{})
	for _, name := range []string{"Reachable", "Server version", "Credential", "Project", "Environment"} {
		if got := statusOf(checks, name); got != doctorOK {
			t.Errorf("%s = %q, want ok; checks: %+v", name, got, checks)
		}
	}
	var out bytes.Buffer
	printDoctor(NewTestOutput(&out, &out, false, false), checks)
	if !strings.Contains(out.String(), "ada@example.com (token from CLAVIS_TOKEN)") {
		t.Fatalf("report:\n%s", out.String())
	}
}

func TestDoctorReportsFixes(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")

	doctorEnv(t, srv.URL, "expired", "dev")
	checks := runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Credential") != doctorFail || statusOf(checks, "Project") != "" {
		t.Fatalf("refused token should fail and stop: %+v", checks)
	}
	if fix := checks[len(checks)-1].Fix; fix != "clavis login --server "+srv.URL {
		t.Fatalf("fix = %q", fix)
	}

	doctorEnv(t, srv.URL, "good", "staging")
	checks = runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Environment") != doctorFail || !strings.Contains(checks[len(checks)-1].Detail, "dev, prod") {
		t.Fatalf("unknown environment: %+v", checks)
	}

	doctorEnv(t, "http://127.0.0.1:9", "good", "dev")
	checks = runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Reachable") != doctorFail || statusOf(checks, "Credential") != "" {
		t.Fatalf("unreachable server should fail and stop: %+v", checks)
	}
}

// doctor runs inside a monorepo app: it finds the .env files at the root and
// beside .clavis.yaml, honours committed_env_files, and only warns, so the
// exit code is left alone.
func TestDoctorReportsEnvFiles(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "good", "dev")
	dir := gitRepo(t,
		map[string]string{
			".env.prod":              "K=3\n",
			"apps/web/.env":          "PORT=3000\n", // Next.js defaults, committed on purpose
			"apps/web/.clavis.yaml":  "committed_env_files: [\".env\"]\n",
			"apps/web/src/README.md": "x\n",
		},
		map[string]string{".env.local": "K=2\n", "apps/web/.env.local": "SECRET=1\n"})
	t.Chdir(filepath.Join(dir, "apps", "web", "src"))

	checks := runDoctor(context.Background(), &Options{})
	exposed := findCheck(checks, ".env files")
	if exposed == nil || exposed.Status != doctorWarn ||
		!strings.HasPrefix(exposed.Fix, "Add to the repository's .gitignore: .env.local apps/web/.env.local (") {
		t.Fatalf(".env files = %+v, want a warning naming .env.local and apps/web/.env.local", exposed)
	}
	tracked := findCheck(checks, ".env in git")
	if tracked == nil || tracked.Status != doctorWarn ||
		!strings.HasPrefix(tracked.Fix, "From the repository root: git rm --cached .env.prod,") {
		t.Fatalf(".env in git = %+v, want a warning naming only .env.prod (apps/web/.env is allowed)", tracked)
	}
	for _, check := range []*doctorCheck{exposed, tracked} {
		if strings.Contains(check.Detail+check.Fix, dir) {
			t.Fatalf("%s leaks the machine's path: %+v", check.Name, check)
		}
	}
	for _, check := range checks {
		if check.Status == doctorFail {
			t.Fatalf("an .env warning must not fail doctor: %+v", checks)
		}
	}
}

// Ignored files produce no line at all.
func TestDoctorQuietWhenEnvFilesIgnored(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "good", "dev")
	t.Chdir(gitRepo(t, nil, map[string]string{".env": "KEY=1\n", ".gitignore": ".env*\n"}))
	checks := runDoctor(context.Background(), &Options{})
	for _, name := range []string{".env files", ".env in git"} {
		if check := findCheck(checks, name); check != nil {
			t.Fatalf("%s = %+v, want no line for an ignored file", name, check)
		}
	}
}

func findCheck(checks []doctorCheck, name string) *doctorCheck {
	for i := range checks {
		if checks[i].Name == name {
			return &checks[i]
		}
	}
	return nil
}
