package cli

import (
	"os"
	"os/exec"
	"sort"
	"strings"
)

// envFilesNotIgnored lists the .env files in the repository root that git
// would commit. A .env holding a key is the most common way a secret leaves a
// developer machine, and everything in this tool is built to keep those out of
// the repository, so doctor says so before the first commit rather than after.
//
// Only names are returned, and only names are ever read: the files' contents
// are never opened here. Outside a git repository there is nothing to report,
// and neither is there when the repository has no such file.
func envFilesNotIgnored(root string) []string {
	var found []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !isEnvFileName(name) {
			continue
		}
		if !gitIgnores(root, name) {
			found = append(found, name)
		}
	}
	sort.Strings(found)
	return found
}

// isEnvFileName matches .env and .env.<something>, but not the committed
// templates that are meant to be read and copied from.
func isEnvFileName(name string) bool {
	if name != ".env" && !strings.HasPrefix(name, ".env.") {
		return false
	}
	switch name {
	case ".env.example", ".env.sample", ".env.template", ".env.dist":
		return false
	}
	return true
}

// gitIgnores reports whether git would ignore the file. `git check-ignore` is
// what decides it, so the answer matches what a commit would actually do,
// including nested .gitignore files and the global excludes file; matching the
// root .gitignore by hand (as warnGitignore does for one exact name) would not.
func gitIgnores(root, name string) bool {
	command := exec.Command("git", "check-ignore", "-q", "--", name)
	command.Dir = root
	return command.Run() == nil
}

// gitRoot returns the top of the working tree containing dir, or "" when dir is
// not inside a repository.
func gitRoot(dir string) string {
	command := exec.Command("git", "rev-parse", "--show-toplevel")
	command.Dir = dir
	raw, err := command.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
