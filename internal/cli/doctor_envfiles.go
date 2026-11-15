package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
)

// A .env holding a key is the most common way a secret leaves a developer
// machine: it gets committed, pushed, and from then on everyone with read
// access to the repository, every fork and every CI log has the key. Doctor
// therefore asks git three questions about the .env files of this project:
//
//   - which ones the next `git add .` would pick up (not ignored),
//   - which ones git already tracks,
//   - which ones were committed in the past, even if removed since.
//
// Only names are read, never contents. git decides every answer, so nested
// .gitignore files, wildcards, negations, .git/info/exclude and the global
// excludes file all behave exactly as they would for a commit.

// envGitTimeout bounds each git call, so a hung git (a credential prompt, a
// huge history) cannot hold doctor.
var envGitTimeout = 5 * time.Second

// envFileStatus is git's answer. Paths are relative to the repository root and
// use forward slashes.
type envFileStatus struct {
	// Exposed are untracked and not ignored.
	Exposed []string
	// Tracked are committed or staged.
	Tracked []string
	// History were committed at some point but are not tracked now.
	History []string
}

// envScan says where to look and what to leave out.
type envScan struct {
	Root string
	// Dirs are the folders to look in, relative to Root ("" is Root itself).
	Dirs []string
	// Allowed are the committed_env_files patterns from .clavis.yaml.
	Allowed []string
}

// envGitError is a git failure worth showing, as opposed to "this is not a
// repository" or "git is not installed", which mean there is nothing to check.
type envGitError struct{ msg string }

func (e *envGitError) Error() string { return e.msg }

// envFileDoctorChecks runs the whole check for doctor from the current folder.
func envFileDoctorChecks(ctx context.Context, configPath string, allowed []string) []doctorCheck {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root, err := gitRoot(ctx, cwd)
	if err != nil {
		return []doctorCheck{envSkipCheck(err)}
	}
	if root == "" {
		return nil
	}
	scan := envScan{Root: root, Dirs: envSearchDirs(root, cwd, configPath), Allowed: allowed}
	status, err := envFilesInGit(ctx, scan)
	if err != nil {
		return []doctorCheck{envSkipCheck(err)}
	}
	// In CI, an untracked .env is almost always one the pipeline just wrote
	// (clavis export -o .env); it is not about to be committed by a person.
	if os.Getenv("CI") != "" {
		status.Exposed = nil
	}
	return envFileChecks(status, envAtRoot(root, cwd))
}

// envSearchDirs is the repository root, the current folder and the folder of
// .clavis.yaml, so a monorepo app with its own .env is covered too.
func envSearchDirs(root, cwd, configPath string) []string {
	dirs := []string{""}
	candidates := []string{cwd}
	if configPath != "" {
		candidates = append(candidates, filepath.Dir(configPath))
	}
	for _, dir := range candidates {
		if rel, ok := relInside(root, dir); ok && !slices.Contains(dirs, rel) {
			dirs = append(dirs, rel)
		}
	}
	return dirs
}

// relInside returns dir relative to root in slash form, and false when dir is
// outside root. Both sides are resolved first: git reports /private/var on
// macOS where os.Getwd may say /var.
func relInside(root, dir string) (string, bool) {
	// Resolve both or neither, so the two sides are always in the same form.
	resolvedDir, errDir := filepath.EvalSymlinks(dir)
	resolvedRoot, errRoot := filepath.EvalSymlinks(root)
	if errDir == nil && errRoot == nil {
		dir, root = resolvedDir, resolvedRoot
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	if rel == "." {
		return "", true
	}
	return filepath.ToSlash(rel), true
}

func envAtRoot(root, cwd string) bool {
	rel, ok := relInside(root, cwd)
	return ok && rel == ""
}

// envFilesInGit asks git the three questions for the folders in scan.
func envFilesInGit(ctx context.Context, scan envScan) (envFileStatus, error) {
	specs := envPathspecs(scan.Dirs)
	keep := func(names []string) []string { return scan.filter(names) }

	exposed, err := runGitNames(ctx, scan.Root, true, append([]string{"ls-files", "-z", "--others", "--exclude-standard", "--"}, specs...)...)
	if err != nil {
		return envFileStatus{}, err
	}
	tracked, err := runGitNames(ctx, scan.Root, true, append([]string{"ls-files", "-z", "--cached", "--"}, specs...)...)
	if err != nil {
		return envFileStatus{}, err
	}
	status := envFileStatus{Exposed: keep(exposed), Tracked: keep(tracked)}

	// --all covers every branch and tag, and HEAD, so a staged removal that
	// is not committed yet still shows up here.
	logged, err := runGitNames(ctx, scan.Root, false,
		append([]string{"-c", "core.quotePath=false", "log", "--all", "--format=", "--name-only", "--"}, specs...)...)
	if err != nil {
		return envFileStatus{}, err
	}
	for _, name := range keep(logged) {
		if !slices.Contains(status.Tracked, name) {
			status.History = append(status.History, name)
		}
	}
	return status, nil
}

// envPathspecs matches .env, .env.<x>, .envrc and <x>.env in each folder,
// ignoring case: on macOS and Windows .ENV is the same file to most tools.
// glob magic keeps * from crossing a slash, so subfolders are not searched.
func envPathspecs(dirs []string) []string {
	var specs []string
	for _, dir := range dirs {
		prefix := ""
		if dir != "" {
			prefix = dir + "/"
		}
		for _, pattern := range []string{".env", ".env.*", ".envrc", "*.env"} {
			specs = append(specs, ":(glob,icase)"+prefix+pattern)
		}
	}
	return specs
}

// filter keeps env files in the scanned folders that are not templates, not
// allowed by .clavis.yaml and not symlinks, sorted and without duplicates.
func (scan envScan) filter(names []string) []string {
	seen := map[string]bool{}
	var kept []string
	for _, name := range names {
		dir := path.Dir(name)
		if dir == "." {
			dir = ""
		}
		if name == "" || seen[name] || !slices.Contains(scan.Dirs, dir) ||
			!isEnvFileName(path.Base(name)) || envAllowed(scan.Allowed, name) {
			continue
		}
		// git stores a symlink as the link, not the file it points to, so a
		// committed .env -> ~/secrets/.env leaks a path, not the keys.
		if info, err := os.Lstat(filepath.Join(scan.Root, filepath.FromSlash(name))); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		seen[name] = true
		kept = append(kept, name)
	}
	sort.Strings(kept)
	return kept
}

// isEnvFileName matches .env, .env.<x>, .envrc and <x>.env in any case, but
// not the templates that are meant to be committed and copied from, such as
// .env.example or example.env.
func isEnvFileName(name string) bool {
	lower := strings.ToLower(name)
	if lower != ".env" && lower != ".envrc" && !strings.HasPrefix(lower, ".env.") && !strings.HasSuffix(lower, ".env") {
		return false
	}
	for _, part := range strings.Split(lower, ".") {
		switch part {
		case "example", "sample", "template", "dist":
			return false
		}
	}
	return true
}

// envAllowed reports whether a committed_env_files pattern covers name.
func envAllowed(patterns []string, name string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimPrefix(strings.TrimSpace(pattern), "/")
		target := path.Base(name)
		if strings.Contains(pattern, "/") {
			target = name
		}
		if ok, err := path.Match(pattern, target); err == nil && ok {
			return true
		}
	}
	return false
}

// gitRoot returns the top of the working tree containing dir. It returns ""
// and no error when dir is not in a repository or git is not installed, and
// an *envGitError when git is there but cannot answer (for example a
// repository owned by another user).
func gitRoot(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, envGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	command.WaitDelay = time.Second
	command.Dir = dir
	var stderr bytes.Buffer
	command.Stderr = &stderr
	raw, err := command.Output()
	if err == nil {
		return strings.TrimSpace(string(raw)), nil
	}
	if errors.Is(err, exec.ErrNotFound) || strings.Contains(stderr.String(), "not a git repository") {
		return "", nil
	}
	return "", gitFailure(ctx, err, stderr.String())
}

// runGitNames runs git in root and splits its output into names, on NUL when
// nulSeparated and on newlines otherwise.
func runGitNames(ctx context.Context, root string, nulSeparated bool, args ...string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, envGitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", args...)
	command.WaitDelay = time.Second
	command.Dir = root
	var stderr bytes.Buffer
	command.Stderr = &stderr
	raw, err := command.Output()
	if err != nil {
		return nil, gitFailure(ctx, err, stderr.String())
	}
	sep := []byte{'\n'}
	if nulSeparated {
		sep = []byte{0}
	}
	var names []string
	for _, field := range bytes.Split(raw, sep) {
		if name := strings.TrimSpace(string(field)); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

func gitFailure(ctx context.Context, err error, stderr string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &envGitError{msg: "git took longer than " + envGitTimeout.String()}
	}
	line, _, _ := strings.Cut(strings.TrimSpace(stderr), "\n")
	line = strings.TrimPrefix(line, "fatal: ")
	if line == "" {
		line = err.Error()
	}
	return &envGitError{msg: "git: " + line}
}

func envSkipCheck(err error) doctorCheck {
	return doctorCheck{Section: "Project", Name: ".env files", Status: doctorSkip,
		Detail: "could not ask git about .env files (" + err.Error() + ")"}
}

// envFileChecks turns git's answer into doctor lines. All are warnings, so
// doctor's exit code still means "a check failed". atRoot says whether doctor
// runs at the repository root, where pasted git commands use these paths as is.
func envFileChecks(status envFileStatus, atRoot bool) []doctorCheck {
	fromRoot := ""
	if !atRoot {
		fromRoot = "From the repository root: "
	}
	var checks []doctorCheck
	if files := status.Exposed; len(files) > 0 {
		importArg := "FILE"
		if len(files) == 1 {
			importArg = files[0]
		}
		checks = append(checks, doctorCheck{Section: "Project", Name: ".env files", Status: doctorWarn,
			Detail: strings.Join(files, ", ") + " " + isAre(files) + " not ignored by git; the next commit could include " + itThem(files),
			Fix: "Add to the repository's .gitignore: " + strings.Join(files, " ") +
				" (or keep the values in Clavis instead: clavis import " + importArg + ")"})
	}
	if files := status.Tracked; len(files) > 0 {
		checks = append(checks, doctorCheck{Section: "Project", Name: ".env in git", Status: doctorWarn,
			Detail: strings.Join(files, ", ") + " " + isAre(files) + " tracked by git; once committed, the values stay in the history",
			Fix: fromRoot + "git rm --cached " + strings.Join(files, " ") + ", add " + itThem(files) +
				" to .gitignore, and rotate the keys (files that only hold defaults can go under committed_env_files in .clavis.yaml)"})
	}
	if files := status.History; len(files) > 0 {
		checks = append(checks, doctorCheck{Section: "Project", Name: ".env history", Status: doctorWarn,
			Detail: strings.Join(files, ", ") + " " + wasWere(files) + " committed in the past; removing a file does not remove it from the history",
			Fix:    "Rotate the keys " + itThem(files) + " held; anyone with a clone still has the old values"})
	}
	return checks
}

func isAre(files []string) string {
	if len(files) == 1 {
		return "is"
	}
	return "are"
}

func wasWere(files []string) string {
	if len(files) == 1 {
		return "was"
	}
	return "were"
}

func itThem(files []string) string {
	if len(files) == 1 {
		return "it"
	}
	return "them"
}
