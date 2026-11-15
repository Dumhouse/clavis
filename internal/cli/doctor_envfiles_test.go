package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A .env holding a key is the most common way a secret leaves a machine, so
// doctor reports the ones git would commit, already tracks, or once had.
// These tests build real repositories because git is what decides.

// hermeticGit makes every git command in the test, including the ones the
// code under test runs, ignore the developer's own config and excludes file.
func hermeticGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("CI", "")
}

// gitRepo creates a repository, commits the files in committed, then writes
// the files in files on top without adding them.
func gitRepo(t *testing.T, committed, files map[string]string) string {
	t.Helper()
	hermeticGit(t)
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	if len(committed) > 0 {
		writeFiles(t, dir, committed)
		runGit(t, dir, "add", "-A")
		runGit(t, dir, "commit", "-q", "-m", "init")
	}
	writeFiles(t, dir, files)
	return dir
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	if raw, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, raw)
	}
}

func TestEnvFilesInGit(t *testing.T) {
	tests := []struct {
		name        string
		committed   map[string]string
		files       map[string]string
		after       func(t *testing.T, dir string)
		dirs        []string // default: the root only
		allowed     []string
		wantExposed []string
		wantTracked []string
		wantHistory []string
	}{
		{
			name:  "ignored .env is not reported",
			files: map[string]string{".env": "KEY=1\n", ".gitignore": "/.env\n"},
		},
		{
			name:        "no .gitignore at all",
			files:       map[string]string{".env": "KEY=1\n"},
			wantExposed: []string{".env"},
		},
		{
			name:        "variants are reported too",
			files:       map[string]string{".env.local": "KEY=1\n", ".env.prod": "K=2\n"},
			wantExposed: []string{".env.local", ".env.prod"},
		},
		{
			name:        "one ignored and one not",
			files:       map[string]string{".env": "A=1\n", ".env.local": "B=2\n", ".gitignore": "/.env\n"},
			wantExposed: []string{".env.local"},
		},
		{
			name:        "direnv and docker compose names",
			files:       map[string]string{".envrc": "export A=1\n", "prod.env": "B=2\n", "app.env": "C=3\n"},
			wantExposed: []string{".envrc", "app.env", "prod.env"},
		},
		{
			name:        "case does not matter",
			files:       map[string]string{".ENV": "A=1\n"},
			wantExposed: []string{".ENV"},
		},
		{
			name: "templates are never reported",
			files: map[string]string{".env.example": "K=\n", ".env.sample": "K=\n", ".env.template": "K=\n",
				".env.dist": "K=\n", "example.env": "K=\n", ".envrc.example": "K=\n", ".env.example.local": "K=\n"},
		},
		{
			name:  "names that only look similar",
			files: map[string]string{"environment.go": "package x\n", ".environment": "A=1\n", "env": "A=1\n"},
		},
		{
			name:  "a wildcard that covers them counts",
			files: map[string]string{".env": "A=1\n", ".env.prod": "B=2\n", ".gitignore": ".env*\n"},
		},
		{
			name:        "a negated entry is honoured",
			files:       map[string]string{".env": "A=1\n", ".gitignore": ".env*\n!.env\n"},
			wantExposed: []string{".env"},
		},
		{
			name:        "a nested .gitignore cannot ignore a root file",
			files:       map[string]string{".env": "A=1\n", "sub/.gitignore": ".env\n"},
			wantExposed: []string{".env"},
		},
		{
			name:  ".git/info/exclude counts",
			files: map[string]string{".env": "A=1\n", ".git/info/exclude": ".env\n"},
		},
		{
			name:  "subfolders are not searched unless asked",
			files: map[string]string{"packages/app/.env": "A=1\n", ".env.d/x": "B=2\n"},
		},
		{
			name:        "the current or .clavis.yaml folder is searched",
			files:       map[string]string{"packages/app/.env": "A=1\n", "packages/app/deep/.env": "B=2\n", "packages/other/.env": "C=3\n"},
			dirs:        []string{"", "packages/app"},
			wantExposed: []string{"packages/app/.env"},
		},
		{
			name:        "a committed .env is tracked even once ignored",
			committed:   map[string]string{".env": "KEY=1\n"},
			files:       map[string]string{".gitignore": ".env\n"},
			wantTracked: []string{".env"},
		},
		{
			name:        "tracked and exposed at once",
			committed:   map[string]string{".env": "KEY=1\n"},
			files:       map[string]string{".env.local": "B=2\n"},
			wantExposed: []string{".env.local"},
			wantTracked: []string{".env"},
		},
		{
			name:      "removed from git but still in the history",
			committed: map[string]string{".env": "KEY=1\n"},
			after: func(t *testing.T, dir string) {
				runGit(t, dir, "rm", "-q", "--cached", ".env")
				writeFiles(t, dir, map[string]string{".gitignore": ".env\n"})
				runGit(t, dir, "add", ".gitignore")
				runGit(t, dir, "commit", "-q", "-m", "stop tracking .env")
			},
			wantHistory: []string{".env"},
		},
		{
			name:      "a staged removal is still in the last commit",
			committed: map[string]string{".env": "KEY=1\n"},
			after: func(t *testing.T, dir string) {
				runGit(t, dir, "rm", "-q", "--cached", ".env")
			},
			wantExposed: []string{".env"},
			wantHistory: []string{".env"},
		},
		{
			name:      "history on another branch counts",
			committed: map[string]string{"README": "x\n"},
			after: func(t *testing.T, dir string) {
				runGit(t, dir, "checkout", "-q", "-b", "leak")
				writeFiles(t, dir, map[string]string{".env.prod": "K=1\n"})
				runGit(t, dir, "add", ".env.prod")
				runGit(t, dir, "commit", "-q", "-m", "oops")
				runGit(t, dir, "checkout", "-q", "main")
			},
			wantHistory: []string{".env.prod"},
		},
		{
			name:      "committed_env_files silences all three",
			committed: map[string]string{".env": "PORT=3000\n", ".env.development": "DEBUG=1\n"},
			files:     map[string]string{".env.test": "X=1\n", ".env.local": "SECRET=1\n"},
			allowed:   []string{".env", ".env.development", "/.env.test"},
			// .env.local is still reported: it is not on the list.
			wantExposed: []string{".env.local"},
		},
		{
			name:    "a pattern with a slash matches the path from the root",
			files:   map[string]string{"config/app.env": "A=1\n", "app.env": "B=2\n"},
			dirs:    []string{"", "config"},
			allowed: []string{"config/*.env"},
			// app.env at the root is not covered by config/*.env.
			wantExposed: []string{"app.env"},
		},
		{
			name:  "no .env at all",
			files: map[string]string{".gitignore": ".env\n"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := gitRepo(t, tt.committed, tt.files)
			if tt.after != nil {
				tt.after(t, dir)
			}
			dirs := tt.dirs
			if dirs == nil {
				dirs = []string{""}
			}
			got, err := envFilesInGit(context.Background(), envScan{Root: dir, Dirs: dirs, Allowed: tt.allowed})
			if err != nil {
				t.Fatalf("envFilesInGit: %v", err)
			}
			if !reflect.DeepEqual(got.Exposed, tt.wantExposed) {
				t.Errorf("Exposed = %q, want %q", got.Exposed, tt.wantExposed)
			}
			if !reflect.DeepEqual(got.Tracked, tt.wantTracked) {
				t.Errorf("Tracked = %q, want %q", got.Tracked, tt.wantTracked)
			}
			if !reflect.DeepEqual(got.History, tt.wantHistory) {
				t.Errorf("History = %q, want %q", got.History, tt.wantHistory)
			}
		})
	}
}

// git commits a symlink as the link itself, so a .env pointing elsewhere
// leaks a path, not the keys.
func TestEnvFilesInGitSkipsSymlinks(t *testing.T) {
	dir := gitRepo(t, nil, nil)
	target := filepath.Join(t.TempDir(), "real.env")
	if err := os.WriteFile(target, []byte("KEY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".env")); err != nil {
		t.Skipf("cannot create symlinks here: %v", err)
	}
	got, err := envFilesInGit(context.Background(), envScan{Root: dir, Dirs: []string{""}})
	if err != nil || got.Exposed != nil {
		t.Fatalf("envFilesInGit = %+v, %v; want nothing for a symlink", got, err)
	}
}

// The contents are never opened: a file the check cannot read is still a
// file git would commit, and reporting its name is the whole job.
func TestEnvFilesInGitReadsOnlyNames(t *testing.T) {
	dir := gitRepo(t, nil, map[string]string{".env": "SUPER_SECRET=abc\n"})
	if err := os.Chmod(filepath.Join(dir, ".env"), 0o000); err != nil {
		t.Skipf("cannot drop read permission here: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, ".env"), 0o600) })
	got, err := envFilesInGit(context.Background(), envScan{Root: dir, Dirs: []string{""}})
	if err != nil || !reflect.DeepEqual(got.Exposed, []string{".env"}) {
		t.Fatalf("envFilesInGit = %+v, %v; want Exposed [.env]", got, err)
	}
}

// Outside a repository there is nothing to check and nothing to say.
func TestGitRootOutsideRepository(t *testing.T) {
	hermeticGit(t)
	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
	root, err := gitRoot(context.Background(), dir)
	if root != "" || err != nil {
		t.Fatalf("gitRoot = %q, %v; want empty and no error", root, err)
	}
}

// Without git installed the check stays quiet too.
func TestGitRootWithoutGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	root, err := gitRoot(context.Background(), t.TempDir())
	if root != "" || err != nil {
		t.Fatalf("gitRoot = %q, %v; want empty and no error", root, err)
	}
}

// fakeGit puts a git on PATH that runs script, for failures a real
// repository cannot produce on demand.
func fakeGit(t *testing.T, script string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake git is a shell script")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil { //nolint:gosec // G306: a test executable
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
}

// A git that is there but refuses (a repository owned by someone else)
// becomes a visible "skipped" line, not silence and not a false warning.
func TestEnvFileDoctorChecksWhenGitRefuses(t *testing.T) {
	fakeGit(t, `echo "fatal: detected dubious ownership in repository at '/srv/app'" >&2; exit 128`)
	t.Chdir(t.TempDir())
	checks := envFileDoctorChecks(context.Background(), "", nil)
	if len(checks) != 1 || checks[0].Status != doctorSkip ||
		!strings.Contains(checks[0].Detail, "detected dubious ownership") {
		t.Fatalf("checks = %+v, want one skip line naming the git error", checks)
	}
}

// A git that hangs is cut off, and doctor says so.
func TestEnvFileDoctorChecksWhenGitHangs(t *testing.T) {
	fakeGit(t, "exec sleep 10")
	restore := envGitTimeout
	envGitTimeout = 100 * time.Millisecond
	t.Cleanup(func() { envGitTimeout = restore })
	t.Chdir(t.TempDir())

	start := time.Now()
	checks := envFileDoctorChecks(context.Background(), "", nil)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("took %s; the timeout did not stop git", elapsed)
	}
	if len(checks) != 1 || checks[0].Status != doctorSkip || !strings.Contains(checks[0].Detail, "longer than") {
		t.Fatalf("checks = %+v, want one skip line about the timeout", checks)
	}
}

// In CI an untracked .env is the one the pipeline just wrote; tracked and
// historical ones still matter there.
func TestEnvFileDoctorChecksInCI(t *testing.T) {
	dir := gitRepo(t, map[string]string{".env.prod": "K=1\n"}, map[string]string{".env": "K=2\n"})
	t.Chdir(dir)
	t.Setenv("CI", "true")
	checks := envFileDoctorChecks(context.Background(), "", nil)
	if findCheck(checks, ".env files") != nil {
		t.Errorf("CI should not report untracked files: %+v", checks)
	}
	if findCheck(checks, ".env in git") == nil {
		t.Errorf("CI should still report tracked files: %+v", checks)
	}
}

func TestEnvSearchDirs(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "packages", "app")
	if err := os.MkdirAll(filepath.Join(app, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	tests := []struct {
		name       string
		cwd        string
		configPath string
		want       []string
	}{
		{"at the root", root, "", []string{""}},
		{"in a subfolder", app, "", []string{"", "packages/app"}},
		{"config in a parent of cwd", filepath.Join(app, "src"), filepath.Join(app, ".clavis.yaml"), []string{"", "packages/app/src", "packages/app"}},
		{"config at the root is not repeated", app, filepath.Join(root, ".clavis.yaml"), []string{"", "packages/app"}},
		{"config outside the repository is ignored", root, filepath.Join(outside, ".clavis.yaml"), []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := envSearchDirs(root, tt.cwd, tt.configPath); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("envSearchDirs = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestEnvAllowed(t *testing.T) {
	tests := []struct {
		patterns []string
		name     string
		want     bool
	}{
		{[]string{".env"}, ".env", true},
		{[]string{".env"}, "packages/app/.env", true},
		{[]string{".env.*"}, ".env.development", true},
		{[]string{"/.env"}, ".env", true},
		{[]string{"config/*.env"}, "config/app.env", true},
		{[]string{"config/*.env"}, "app.env", false},
		{[]string{"packages/app/.env"}, ".env", false},
		{[]string{"["}, ".env", false}, // a bad pattern matches nothing
		{nil, ".env", false},
	}
	for _, tt := range tests {
		if got := envAllowed(tt.patterns, tt.name); got != tt.want {
			t.Errorf("envAllowed(%q, %q) = %v, want %v", tt.patterns, tt.name, got, tt.want)
		}
	}
}

// gitRoot walks up, so doctor run from a folder inside the repository finds
// the root and the files beside it.
func TestGitRootWalksUp(t *testing.T) {
	dir := gitRepo(t, nil, nil)
	sub := filepath.Join(dir, "cmd", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(dir) // macOS: /var is /private/var
	if err != nil {
		t.Fatal(err)
	}
	root, err := gitRoot(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	got, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("gitRoot(%q) = %q: %v", sub, root, err)
	}
	if got != want {
		t.Fatalf("gitRoot(%q) = %q, want %q", sub, got, want)
	}
}

// The fix lines are commands people paste, so they must be exact.
func TestEnvFileChecksWording(t *testing.T) {
	status := envFileStatus{
		Exposed: []string{".env", ".env.local"},
		Tracked: []string{".env.prod"},
		History: []string{".env.old"},
	}
	checks := envFileChecks(status, true)
	if len(checks) != 3 {
		t.Fatalf("got %d checks, want 3: %+v", len(checks), checks)
	}
	want := []doctorCheck{
		{Section: "Project", Name: ".env files", Status: doctorWarn,
			Detail: ".env, .env.local are not ignored by git; the next commit could include them",
			Fix:    "Add to the repository's .gitignore: .env .env.local (or keep the values in Clavis instead: clavis import FILE)"},
		{Section: "Project", Name: ".env in git", Status: doctorWarn,
			Detail: ".env.prod is tracked by git; once committed, the values stay in the history",
			Fix:    "git rm --cached .env.prod, add it to .gitignore, and rotate the keys (files that only hold defaults can go under committed_env_files in .clavis.yaml)"},
		{Section: "Project", Name: ".env history", Status: doctorWarn,
			Detail: ".env.old was committed in the past; removing a file does not remove it from the history",
			Fix:    "Rotate the keys it held; anyone with a clone still has the old values"},
	}
	if !reflect.DeepEqual(checks, want) {
		for i := range want {
			if checks[i] != want[i] {
				t.Errorf("check %d =\n  %+v\nwant\n  %+v", i, checks[i], want[i])
			}
		}
	}

	one := envFileChecks(envFileStatus{Exposed: []string{"packages/app/.env"}, Tracked: []string{"packages/app/.env.prod"}}, false)
	if got := one[0].Fix; !strings.HasSuffix(got, "clavis import packages/app/.env)") {
		t.Errorf("one exposed file should be named in the import hint: %q", got)
	}
	if got := one[1].Fix; !strings.HasPrefix(got, "From the repository root: git rm --cached packages/app/.env.prod,") {
		t.Errorf("away from the root, git commands should say where to run them: %q", got)
	}
	if got := envFileChecks(envFileStatus{}, true); len(got) != 0 {
		t.Errorf("nothing to report gave %+v", got)
	}
}

func TestWarnGitignore(t *testing.T) {
	tests := []struct {
		name      string
		committed map[string]string
		files     map[string]string
		want      string // substring of the warning; "" means no warning
	}{
		{"ignored by a wildcard", nil, map[string]string{".gitignore": ".env*\n"}, ""},
		{"not ignored", nil, map[string]string{".gitignore": "node_modules/\n"}, "is not ignored by git"},
		{"no .gitignore", nil, nil, "is not ignored by git"},
		{"already tracked", map[string]string{".env": "OLD=1\n"}, map[string]string{".gitignore": ".env\n"}, "is tracked by git"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := gitRepo(t, tt.committed, tt.files)
			writeFiles(t, dir, map[string]string{".env": "KEY=1\n"})
			var buf bytes.Buffer
			warnGitignore(context.Background(), newOutput(&buf, &buf, false, false), filepath.Join(dir, ".env"))
			got := buf.String()
			if tt.want == "" && got != "" {
				t.Fatalf("want no warning, got %q", got)
			}
			if tt.want != "" && !strings.Contains(got, tt.want) {
				t.Fatalf("warning = %q, want it to contain %q", got, tt.want)
			}
		})
	}

	t.Run("outside a repository", func(t *testing.T) {
		hermeticGit(t)
		dir := t.TempDir()
		t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))
		var buf bytes.Buffer
		warnGitignore(context.Background(), newOutput(&buf, &buf, false, false), filepath.Join(dir, ".env"))
		if buf.Len() != 0 {
			t.Fatalf("want no warning outside a repository, got %q", buf.String())
		}
	})
}
