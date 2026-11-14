package cli

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	var run func(*cobra.Command, []string) error
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an environment's secrets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			err := run(cmd, args)
			if err != nil && format == "github-env" {
				// Show the cause and fix on the CI run page, not only in the log.
				fmt.Fprintln(opts.output().Out, ciErrorAnnotation(err))
			}
			return err
		},
	}
	run = func(cmd *cobra.Command, _ []string) error {
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
		kinds := map[string]string{}
		for _, secret := range result.Secrets {
			if variablesOnly && secret.Kind != "variable" {
				continue
			}
			values[secret.Key] = secret.Value
			kinds[secret.Key] = secret.Kind
		}
		if format == "github-env" {
			return exportCIEnv(out, outPath, values, kinds)
		}
		content, err := encodeExport(format, resolved, values)
		if err != nil {
			return err
		}

		if outPath == "" || outPath == "-" {
			_, err := fmt.Fprint(out.Out, content)
			return err
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
	}
	cmd.Flags().StringVar(&format, "format", "dotenv", "dotenv, json, yaml, shell, docker, k8s, or github-env (CI job environment on GitHub, Forgejo and Gitea)")
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
		return "", fmt.Errorf("unknown format %q (dotenv, json, yaml, shell, docker, k8s, github-env)", format)
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

// exportCIEnv appends values to the job environment file of GitHub, Forgejo
// and Gitea Actions ($GITHUB_ENV), so later steps see them as environment
// variables. Each value uses the multiline form NAME<<DELIMITER with a random
// delimiter, so newlines and "=" survive. Secret values are masked in the job
// log first (one ::add-mask:: per line); variables are left readable.
func exportCIEnv(out *Output, path string, values, kinds map[string]string) error {
	if path == "" || path == "-" {
		path = os.Getenv("GITHUB_ENV")
	}
	if path == "" {
		return errors.New("--format github-env writes to $GITHUB_ENV, which is not set: run it inside a GitHub, Forgejo or Gitea Actions job, or pass -o FILE")
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var b strings.Builder
	exported := 0
	var names []string
	for _, key := range keys {
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GITHUB_") || strings.HasPrefix(upper, "GITEA_") || strings.HasPrefix(upper, "FORGEJO_") || strings.HasPrefix(upper, "RUNNER_") {
			out.Warn("Skipped %s: runners reserve names starting with GITHUB_, GITEA_, FORGEJO_ and RUNNER_.", key)
			continue
		}
		value := values[key]
		if kinds[key] != "variable" {
			for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
				if strings.TrimSpace(line) != "" {
					fmt.Fprintf(out.Out, "::add-mask::%s\n", line)
				}
			}
		}
		delimiter, err := randomDelimiter(value)
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "%s<<%s\n%s\n%s\n", key, delimiter, value, delimiter)
		exported++
		names = append(names, key)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600) //nolint:gosec // G304: the runner's env file
	if err != nil {
		return fmt.Errorf("could not open %s: %w", path, err)
	}
	if _, err := file.WriteString(b.String()); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if exported == 0 {
		// An annotation, so an empty environment is visible on the run page.
		fmt.Fprintln(out.Out, "::warning::Clavis returned no values for this environment. Check that the environment has keys and that the token's environment is the one you meant.")
		return nil
	}
	out.Info("Exported %d values to the job environment: %s", exported, strings.Join(names, ", "))
	return nil
}

func randomDelimiter(value string) (string, error) {
	for {
		raw := make([]byte, 12)
		if _, err := rand.Read(raw); err != nil {
			return "", err
		}
		delimiter := "CLAVIS_" + hex.EncodeToString(raw)
		if !strings.Contains(value, delimiter) {
			return delimiter, nil
		}
	}
}

// ciErrorAnnotation turns an export failure in a CI job into a workflow
// error annotation (GitHub, Forgejo and Gitea Actions) with the likely cause
// and what to change.
func ciErrorAnnotation(err error) string {
	env := os.Getenv("CLAVIS_ENV")
	if env == "" {
		env = "the environment"
	}
	server := os.Getenv("CLAVIS_SERVER")
	if server == "" {
		server = "the server"
	}
	detail := strings.ReplaceAll(err.Error(), "\n", " ")
	title, message := "export failed", "Could not load "+env+"."
	lower := strings.ToLower(detail)
	switch code := ExitCode(err); {
	case code == ExitAuth:
		title, message = "token rejected", "The token was not accepted: it is wrong, expired or revoked. Check the secret passed as the token, or rotate the token in Clavis (Agents & machines) and update the secret."
	case code == ExitPermission:
		title, message = "no access to "+env, "The token is valid but cannot read "+env+" of this project. Each CI token reads one environment: check that the environment matches the token, and that the project and org are right."
	case code == ExitNotFound:
		title, message = "not found", "The org, project or environment "+env+" does not exist. Check the names against the slugs in Clavis."
	case strings.Contains(lower, "x509") || strings.Contains(lower, "certificate") || strings.Contains(lower, "fingerprint"):
		title, message = "certificate not trusted", "The runner does not trust the server's certificate. For a self-signed server set CLAVIS_SERVER_FINGERPRINT (from 'clavis admin tls-info'); for your own CA set CLAVIS_CA_CERT."
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no route to host") || strings.Contains(lower, "timeout") || strings.Contains(lower, "network is unreachable"):
		title, message = "server unreachable", "The runner could not reach "+server+". Check the server address and that this runner's network can reach it."
	case strings.Contains(lower, "no project context"):
		title, message = "which project?", "No project or org: check out the repository with its .clavis.yaml first, or set CLAVIS_PROJECT and CLAVIS_ORG."
	case strings.Contains(lower, "no token") || strings.Contains(lower, "not logged in") || strings.Contains(lower, "sign in"):
		title, message = "no token", "No token: if you pass ${{ secrets.CLAVIS_TOKEN_… }}, that secret is not set in this repository (check the exact name), or the run comes from a fork, which gets no secrets."
	}
	// Workflow commands end at a newline; % and line breaks must be escaped.
	escape := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return "::error title=Clavis: " + escape.Replace(title) + "::" + escape.Replace(message+" (clavis said: "+detail+")")
}
