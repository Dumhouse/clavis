package cli

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// scanHistory looks for leaked tokens and secret values in every line that
// any commit on any branch ever added, so a secret that was committed and
// later deleted is still found. Values are matched in memory and never
// printed. It returns the findings and how many commits it read.
func scanHistory(ctx context.Context, dir string, values map[string]string) ([]scanFinding, int, error) {
	command := exec.CommandContext(ctx, "git", "log", "--all", "--no-color", "--no-ext-diff", "-p", "-U0", "--format=commit %H") //nolint:gosec // G204: fixed arguments
	command.Dir = dir
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	if err := command.Start(); err != nil {
		return nil, 0, fmt.Errorf("git log: %w", err)
	}
	findings, commits := scanPatchStream(stdout, values)
	if err := command.Wait(); err != nil {
		return nil, 0, fmt.Errorf("git log: %w (is this a git repository?)", err)
	}
	return findings, commits, nil
}

// scanPatchStream reads `git log -p -U0` output and scans the added lines.
func scanPatchStream(stream interface{ Read([]byte) (int, error) }, values map[string]string) ([]scanFinding, int) {
	scanner := bufio.NewScanner(stream)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)
	var (
		findings []scanFinding
		commits  int
		commit   string
		file     string
		line     int
	)
	seen := map[string]bool{}
	for scanner.Scan() {
		text := scanner.Text()
		switch {
		case strings.HasPrefix(text, "commit ") && len(text) >= 47:
			commit = text[len("commit "):]
			commits++
		case strings.HasPrefix(text, "+++ "):
			file = strings.TrimPrefix(strings.TrimPrefix(text, "+++ "), "b/")
		case strings.HasPrefix(text, "@@"):
			if m := hunkHeader.FindStringSubmatch(text); m != nil {
				line, _ = strconv.Atoi(m[1])
			}
		case strings.HasPrefix(text, "+"):
			for _, finding := range scanBytes([]byte(text[1:]), file, values) {
				finding.Line = line
				finding.Commit = commit
				id := fmt.Sprintf("%s|%s|%d|%s|%s", commit, finding.File, finding.Line, finding.Type, finding.Key)
				if !seen[id] {
					seen[id] = true
					findings = append(findings, finding)
				}
			}
			line++
		}
	}
	return findings, commits
}
