package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A .env holding a key is the most common way a secret leaves a machine, so
// doctor reports the ones git would commit. These tests build real repositories
// because `git check-ignore` is what decides the answer.

func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	if _, ok := files[".gitignore"]; !ok {
		files[".gitignore"] = "/.env\n"
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	raw, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, raw)
	}
	return strings.TrimSpace(string(raw))
}

func TestEnvFilesNotIgnored(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{
			name:  "ignored .env is not reported",
			files: map[string]string{".env": "KEY=1\n", ".gitignore": "/.env\n"},
			want:  nil,
		},
		{
			name:  "a .env git would commit is reported",
			files: map[string]string{".env": "KEY=1\n", ".gitignore": "node_modules/\n"},
			want:  []string{".env"},
		},
		{
			name:  "variants are reported too",
			files: map[string]string{".env.local": "KEY=1\n", ".env.prod": "K=2\n", ".gitignore": "node_modules/\n"},
			want:  []string{".env.local", ".env.prod"},
		},
		{
			name:  "one ignored and one not",
			files: map[string]string{".env": "A=1\n", ".env.local": "B=2\n", ".gitignore": "/.env\n"},
			want:  []string{".env.local"},
		},
		{
			name:  "templates are never reported",
			files: map[string]string{".env.example": "K=\n", ".env.sample": "K=\n", ".gitignore": "node_modules/\n"},
			want:  nil,
		},
		{
			name:  "a wildcard that covers them counts",
			files: map[string]string{".env": "A=1\n", ".env.prod": "B=2\n", ".gitignore": ".env*\n"},
			want:  nil,
		},
		{
			name:  "a negated entry is honoured",
			files: map[string]string{".env": "A=1\n", ".gitignore": ".env*\n!.env\n"},
			want:  []string{".env"},
		},
		{
			name:  "no .env at all",
			files: map[string]string{".gitignore": ".env\n"},
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := gitRepo(t, tt.files)
			if got := envFilesNotIgnored(dir); strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("envFilesNotIgnored = %v, want %v", got, tt.want)
			}
		})
	}
}

// Outside a git repository there is nothing to check, so nothing is reported.
func TestEnvFilesNotIgnoredOutsideGit(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("KEY=1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if root := gitRoot(dir); root != "" {
		t.Fatalf("gitRoot(%q) = %q, want empty outside a repository", dir, root)
	}
}

// The contents are never opened: a file the check cannot read is still a file
// git would commit, and reporting its name is the whole job.
func TestEnvFilesNotIgnoredReadsOnlyNames(t *testing.T) {
	dir := gitRepo(t, map[string]string{".env": "SUPER_SECRET=abc\n", ".gitignore": "node_modules/\n"})
	if err := os.Chmod(filepath.Join(dir, ".env"), 0o000); err != nil {
		t.Skipf("cannot drop read permission here: %v", err)
	}
	if got := envFilesNotIgnored(dir); strings.Join(got, ",") != ".env" {
		t.Fatalf("envFilesNotIgnored = %v, want [.env]", got)
	}
}

// gitRoot walks up, so doctor run from a folder inside the repository finds the
// root and the files beside it.
func TestGitRootWalksUp(t *testing.T) {
	dir := gitRepo(t, map[string]string{".env": "K=1\n", ".gitignore": "node_modules/\n"})
	sub := filepath.Join(dir, "cmd", "app")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	root := gitRoot(sub)
	if root == "" {
		t.Fatal("gitRoot found nothing inside a repository")
	}
	// macOS resolves /var to /private/var; compare the tail.
	if !strings.HasSuffix(root, filepath.Base(dir)) && root != dir {
		t.Fatalf("gitRoot(%q) = %q, want %q", sub, root, dir)
	}
}
