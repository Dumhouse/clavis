package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newExportCommand(opts *Options) *cobra.Command {
	var format, outPath string
	var variablesOnly bool
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an environment's secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result secretsListResponse
			path := secretsBase(resolved)
			if !variablesOnly {
				path += ":resolve?channel=export"
			}
			if opts.Overlay != "" {
				if !variablesOnly {
					path += "&overlay=" + url.QueryEscape(opts.Overlay)
				} else {
					path += "?overlay=" + url.QueryEscape(opts.Overlay)
				}
			}
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}

			values := map[string]string{}
			for _, secret := range result.Secrets {
				if variablesOnly && secret.Kind != "variable" {
					continue
				}
				values[secret.Key] = secret.Value
			}
			content, err := encodeExport(format, resolved, values)
			if err != nil {
				return err
			}

			if outPath == "" || outPath == "-" {
				fmt.Fprint(out.Out, content)
				return nil
			}
			if err := os.WriteFile(outPath, []byte(content), 0o600); err != nil {
				return fmt.Errorf("could not write %s: %w", outPath, err)
			}
			if err := os.Chmod(outPath, 0o600); err != nil {
				return err
			}
			out.Warn("Wrote secrets to %s (0600). Make sure it is listed in .gitignore.", outPath)
			warnGitignore(out, outPath)
			return nil
		},
	}
	cmd.Flags().StringVar(&format, "format", "dotenv", "dotenv, json, yaml, shell, docker, or k8s")
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default stdout, - for stdout)")
	cmd.Flags().BoolVar(&variablesOnly, "variables-only", false, "export only variables (no read-values permission needed)")
	return cmd
}

func encodeExport(format string, r *resolvedContext, values map[string]string) (string, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	switch format {
	case "dotenv", "docker":
		var b strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&b, "%s=%s\n", key, dotenvQuote(values[key]))
		}
		return b.String(), nil
	case "json":
		b, err := json.MarshalIndent(values, "", "  ")
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	case "yaml":
		b, err := yaml.Marshal(values)
		if err != nil {
			return "", err
		}
		return string(b), nil
	case "shell":
		var b strings.Builder
		for _, key := range keys {
			fmt.Fprintf(&b, "export %s=%s\n", key, shellQuote(values[key]))
		}
		return b.String(), nil
	case "k8s":
		name := r.Project + "-" + r.Env
		manifest := map[string]any{
			"apiVersion": "v1",
			"kind":       "Secret",
			"metadata": map[string]any{
				"name":      name,
				"namespace": "default",
			},
			"type":       "Opaque",
			"stringData": values,
		}
		b, err := yaml.Marshal(manifest)
		if err != nil {
			return "", err
		}
		return string(b), nil
	default:
		return "", fmt.Errorf("unknown format %q (dotenv, json, yaml, shell, docker, k8s)", format)
	}
}

func dotenvQuote(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\n\r\"'#$\\") {
		return value
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + replacer.Replace(value) + `"`
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func warnGitignore(out *Output, path string) {
	ignore, err := os.ReadFile(".gitignore")
	if err != nil {
		out.Warn("No .gitignore found. Do not commit %s.", filepath.Base(path))
		return
	}
	base := filepath.Base(path)
	for _, line := range strings.Split(string(ignore), "\n") {
		if strings.TrimSpace(line) == base || strings.TrimSpace(line) == "/"+base {
			return
		}
	}
	out.Warn("%s is not in .gitignore. Add it before committing.", base)
}
