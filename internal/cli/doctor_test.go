package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

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
		case r.Header.Get("Authorization") == "Bearer limited" && r.URL.Path == "/api/v1/auth/me":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
		case r.Header.Get("Authorization") != "Bearer good" && r.Header.Get("Authorization") != "Bearer limited":
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"title":"Unauthorized","status":401}`))
		case r.URL.Path == "/api/v1/auth/me":
			_, _ = w.Write([]byte(`{"user":{"email":"ada@example.com","name":"Ada"},"orgs":[]}`))
		case r.URL.Path == "/api/v1/orgs/acme/projects/web/environments/dev/secrets":
			_, _ = w.Write([]byte(`{"secrets":[{"key":"OLD","updated_at":"2020-01-01T00:00:00Z"},{"key":"NEW","updated_at":"` + time.Now().Format(time.RFC3339) + `"},{"key":"INH","inherited":true,"updated_at":"2019-01-01T00:00:00Z"}]}`))
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

func TestDoctorConsoleAndHookChecks(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "good", "dev")

	// Outside a git repository the hook check does not apply.
	checks := runDoctor(context.Background(), &Options{})
	if got := statusOf(checks, "Commit hook"); got != "" {
		t.Fatalf("hook check outside a repository = %q", got)
	}
	want := doctorOK
	if !ptySupported {
		want = doctorWarn
	}
	if got := statusOf(checks, "Consoles"); got != want {
		t.Fatalf("Consoles = %q, want %q", got, want)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	if out, err := exec.Command("git", "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	checks = runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Commit hook") != doctorWarn {
		t.Fatalf("repository without a hook should warn: %+v", checks)
	}
	if err := installScanHook(); err != nil {
		t.Fatal(err)
	}
	checks = runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Commit hook") != doctorOK {
		t.Fatalf("installed hook should pass: %+v", checks)
	}
}

func TestDoctorWarnsAboutOldSecrets(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "good", "dev")
	checks := runDoctor(context.Background(), &Options{})
	var age doctorCheck
	for _, check := range checks {
		if check.Name == "Secret age" {
			age = check
		}
	}
	if age.Status != doctorWarn || !strings.Contains(age.Detail, "1 of 2 secrets") || !strings.Contains(age.Detail, "OLD") {
		t.Fatalf("secret age check: %+v", age)
	}
	if strings.Contains(age.Detail, "INH") {
		t.Fatalf("inherited secrets must not count: %+v", age)
	}
}

func TestDoctorAcceptsALimitedToken(t *testing.T) {
	srv := fakeClavis(t, "0.3.2")
	doctorEnv(t, srv.URL, "limited", "dev")
	checks := runDoctor(context.Background(), &Options{})
	if statusOf(checks, "Credential") != doctorOK {
		t.Fatalf("a limited token is valid: %+v", checks)
	}
	if statusOf(checks, "Project") != doctorOK {
		t.Fatalf("checks should go on to the project: %+v", checks)
	}
}
