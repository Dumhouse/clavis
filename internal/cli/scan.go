package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/authn"
)

var clavisTokenPattern = regexp.MustCompile(`clv_(?:mt|at|pat)_[A-Za-z0-9]{8,}`)

type scanFinding struct {
	Type string `json:"type"`
	File string `json:"file"`
	Line int    `json:"line"`
	Key  string `json:"key,omitempty"`
}

func newScanCommand(opts *Options) *cobra.Command {
	var path string
	var staged, installHook, values bool
	cmd := &cobra.Command{
		Use:   "scan [PATH]",
		Short: "Scan files for leaked Clavis tokens and secret values",
		Long: "Scan a directory or git index for valid Clavis token formats. With\n" +
			"read-values permission, also match resolved values in memory. Values are\n" +
			"never printed. Exit code 7 means a leak was found.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				path = args[0]
			}
			if path == "" {
				path = "."
			}
			out := opts.output()
			if installHook {
				if err := installScanHook(); err != nil {
					return err
				}
				out.Success("Installed .git/hooks/pre-commit (clavis scan --staged).")
				return nil
			}
			files, err := scanFiles(path, staged)
			if err != nil {
				return err
			}
			valuesByKey := map[string]string{}
			if values {
				if resolved, err := opts.resolveContext(false); err == nil && resolved.Org != "" && resolved.Project != "" {
					if c, err := resolved.newClient(); err == nil {
						var result secretsListResponse
						if err := c.Get(cmd.Context(), secretsBase(resolved)+":resolve?channel=cli", &result); err == nil {
							for _, secret := range result.Secrets {
								if len(secret.Value) >= 4 {
									valuesByKey[secret.Key] = secret.Value
								}
							}
						} else {
							out.Warn("Could not read values; scanning token formats only.")
						}
					}
				}
			}
			findings := make([]scanFinding, 0)
			for _, file := range files {
				data, err := os.ReadFile(file)
				if err != nil || len(data) > 2<<20 || bytes.IndexByte(data, 0) >= 0 {
					continue
				}
				findings = append(findings, scanBytes(data, file, valuesByKey)...)
			}
			sort.Slice(findings, func(i, j int) bool {
				if findings[i].File != findings[j].File {
					return findings[i].File < findings[j].File
				}
				return findings[i].Line < findings[j].Line
			})
			if opts.JSON {
				if err := out.PrintJSON(map[string]any{"leaks": findings}); err != nil {
					return err
				}
			} else if len(findings) == 0 {
				out.Success("No leaked tokens or secret values found in %d files.", len(files))
			} else {
				rows := make([][]string, 0, len(findings))
				for _, finding := range findings {
					detail := finding.Type
					if finding.Key != "" {
						detail += " " + finding.Key
					}
					rows = append(rows, []string{finding.File, fmt.Sprintf("%d", finding.Line), detail})
				}
				out.Table([]string{"FILE", "LINE", "FINDING"}, rows)
				out.Errorf("%d potential leak(s) found.", len(findings))
			}
			if len(findings) > 0 {
				return &ExitError{Code: ExitLeak, Err: fmt.Errorf("%d leak(s) found", len(findings)), Silent: true}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&staged, "staged", false, "scan git staged files")
	cmd.Flags().BoolVar(&installHook, "install-hook", false, "install a pre-commit hook that runs the staged scan")
	cmd.Flags().BoolVar(&values, "values", true, "also match resolved secret values in memory (requires read-values)")
	return cmd
}

func scanFiles(path string, staged bool) ([]string, error) {
	if staged {
		command := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR", "-z")
		raw, err := command.Output()
		if err != nil {
			return nil, fmt.Errorf("git staged files: %w", err)
		}
		var files []string
		for _, name := range strings.Split(string(raw), "\x00") {
			if name != "" {
				files = append(files, name)
			}
		}
		return files, nil
	}
	var files []string
	err := filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "node_modules", "dist", "build", "vendor", ".next":
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil || info.Size() > 2<<20 {
			return nil
		}
		files = append(files, candidate)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func scanBytes(data []byte, filename string, values map[string]string) []scanFinding {
	findings := make([]scanFinding, 0)
	for _, match := range clavisTokenPattern.FindAllIndex(data, -1) {
		token := string(data[match[0]:match[1]])
		if _, err := authn.ParseToken(token); err != nil {
			continue
		}
		findings = append(findings, scanFinding{
			Type: "clavis-token", File: filename, Line: lineAt(data, match[0]),
		})
	}
	if len(values) > 0 {
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value := values[key]
			offset := 0
			for {
				index := bytes.Index(data[offset:], []byte(value))
				if index < 0 {
					break
				}
				position := offset + index
				findings = append(findings, scanFinding{
					Type: "secret-value", File: filename, Line: lineAt(data, position), Key: key,
				})
				offset = position + len(value)
			}
		}
	}
	return findings
}

func lineAt(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	return bytes.Count(data[:offset], []byte{'\n'}) + 1
}

func installScanHook() error {
	command := exec.Command("git", "rev-parse", "--git-dir")
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}
	gitDir := strings.TrimSpace(string(raw))
	hookPath := filepath.Join(gitDir, "hooks", "pre-commit")
	if existing, err := os.ReadFile(hookPath); err == nil && !bytes.Contains(existing, []byte("clavis scan --staged")) {
		return fmt.Errorf("%s already exists; merge the clavis scan line manually", hookPath)
	}
	if err := os.MkdirAll(filepath.Dir(hookPath), 0o755); err != nil {
		return err
	}
	script := "#!/bin/sh\n# Installed by clavis scan --install-hook\nexec clavis scan --staged\n"
	if err := os.WriteFile(hookPath, []byte(script), 0o755); err != nil {
		return err
	}
	return nil
}
