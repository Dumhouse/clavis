package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newImportCommand(opts *Options) *cobra.Command {
	var kind, message string
	var dryRun bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "import FILE",
		Short: "Import a .env, JSON, or YAML file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}

			entries, err := collectImportEntries(args[0], kind)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("no key/value pairs found in %s", args[0])
			}

			if dryRun {
				for _, entry := range entries {
					prefix := ""
					if entry.folder != "" {
						prefix = entry.folder + " "
					}
					fmt.Fprintln(out.Out, prefix+entry.key)
				}
				out.Info("%d keys would be imported into %s/%s/%s.", len(entries), resolved.Org, resolved.Project, resolved.Env)
				return nil
			}

			if err := requireProductionConfirmation(cmd.Context(), opts, c, resolved, "import "+args[0], yes); err != nil {
				return err
			}
			imported := 0
			for _, entry := range entries {
				entryMessage := message
				if entryMessage == "" {
					entryMessage = "Imported from " + entry.source
				}
				if _, _, err := upsertSecret(cmd.Context(), c, resolved, entry.key, entry.value, kind, "", entryMessage, entry.folder); err != nil {
					return fmt.Errorf("import %s: %w", entry.key, err)
				}
				imported++
			}
			out.Success("Imported %d keys into %s/%s/%s", imported, resolved.Org, resolved.Project, resolved.Env)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "secret", "secret, variable, or file")
	cmd.Flags().StringVar(&message, "message", "", "change message recorded in history")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "list keys without writing")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	return cmd
}

func readAll(r *os.File) ([]byte, error) {
	var buf bytes.Buffer
	_, err := buf.ReadFrom(r)
	return buf.Bytes(), err
}

// ParseImportFile selects a parser by extension, defaulting to dotenv.
func parseImportFile(path string, data []byte) (map[string]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json":
		return parseJSONValues(data)
	case ".yaml", ".yml":
		return parseYAMLValues(data)
	default:
		return parseDotenv(data)
	}
}

func parseJSONValues(data []byte) (map[string]string, error) {
	out := map[string]string{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return out, nil
}

func parseYAMLValues(data []byte) (map[string]string, error) {
	out := map[string]string{}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse YAML: %w", err)
	}
	return out, nil
}

// ParseDotenv parses .env content. It supports comments, `export ` prefixes,
// single/double quotes, and common escapes. It never executes code.
func ParseDotenv(data []byte) (map[string]string, error) { return parseDotenv(data) }

func parseDotenv(data []byte) (map[string]string, error) {
	out := map[string]string{}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", lineNo)
		}
		key := strings.TrimSpace(line[:eq])
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		raw := strings.TrimSpace(line[eq+1:])
		value, err := parseDotenvValue(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		out[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read dotenv: %w", err)
	}
	return out, nil
}

func parseDotenvValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	switch raw[0] {
	case '"':
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated double quote")
		}
		inner := raw[1 : len(raw)-1]
		var b strings.Builder
		for i := 0; i < len(inner); i++ {
			if inner[i] != '\\' {
				b.WriteByte(inner[i])
				continue
			}
			i++
			if i >= len(inner) {
				return "", fmt.Errorf("trailing backslash")
			}
			switch inner[i] {
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			default:
				b.WriteByte(inner[i])
			}
		}
		return b.String(), nil
	case '\'':
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated single quote")
		}
		return raw[1 : len(raw)-1], nil
	default:
		// Strip an inline comment only when preceded by whitespace.
		for i := 1; i < len(raw); i++ {
			if raw[i] == '#' && unicode.IsSpace(rune(raw[i-1])) {
				raw = strings.TrimSpace(raw[:i])
				break
			}
		}
		return raw, nil
	}
}

type importEntry struct {
	key    string
	value  string
	folder string
	source string
}

// collectImportEntries accepts a file or a directory. Directories are walked
// recursively; each file's parent directory becomes the secret folder.
func collectImportEntries(path, _ string) ([]importEntry, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var files []string
	root := ""
	if info.IsDir() {
		root = path
		err = filepath.WalkDir(path, func(candidate string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == ".git" {
					return filepath.SkipDir
				}
				return nil
			}
			if importableFile(entry.Name()) {
				files = append(files, candidate)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	} else {
		files = []string{path}
	}
	sort.Strings(files)
	var out []importEntry
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		values, err := parseImportFile(file, data)
		if err != nil {
			return nil, err
		}
		folder := ""
		if root != "" {
			rel, err := filepath.Rel(root, filepath.Dir(file))
			if err == nil && rel != "." {
				folder = "/" + filepath.ToSlash(rel)
			}
		}
		keys := make([]string, 0, len(values))
		for key := range values {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			out = append(out, importEntry{key: key, value: values[key], folder: folder, source: file})
		}
	}
	return out, nil
}

func importableFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".env") {
		return true
	}
	switch filepath.Ext(lower) {
	case ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}
