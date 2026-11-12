package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const defaultSchemaFile = "clavis.schema.yaml"

type schemaFile struct {
	Version int          `json:"version" yaml:"version"`
	Schema  []schemaJSON `json:"schema" yaml:"schema"`
}

func newSchemaCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "schema",
		Short: "Manage the project secret schema",
		Long: "Keep the typed configuration contract in your repository. Pull, edit,\n" +
			"and push it with code review, or infer a starting point from existing keys.",
	}
	cmd.AddCommand(newSchemaListCommand(opts))
	cmd.AddCommand(newSchemaPullCommand(opts))
	cmd.AddCommand(newSchemaPushCommand(opts))
	cmd.AddCommand(newSchemaInferCommand(opts))
	return cmd
}

func newSchemaListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List schema entries",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var schema schemaResponse
			if err := c.Get(cmd.Context(), schemaBase(resolved), &schema); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(schema.Schema)
			}
			if len(schema.Schema) == 0 {
				out.Info("No schema entries. Run `clavis schema infer` to draft one.")
				return nil
			}
			rows := make([][]string, 0, len(schema.Schema))
			for _, entry := range schema.Schema {
				required := requiredSummary(entry.Required)
				rows = append(rows, []string{entry.Key, entry.Type, entry.Kind, required, entry.Description})
			}
			out.Table([]string{"KEY", "TYPE", "KIND", "REQUIRED", "DESCRIPTION"}, rows)
			return nil
		},
	}
}

func requiredSummary(required map[string]bool) string {
	if len(required) == 0 {
		return ""
	}
	envs := make([]string, 0, len(required))
	for env, value := range required {
		if value {
			envs = append(envs, env)
		}
	}
	sort.Strings(envs)
	return strings.Join(envs, ",")
}

func newSchemaPullCommand(opts *Options) *cobra.Command {
	var outPath string
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Download the schema as code",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var schema schemaResponse
			if err := c.Get(cmd.Context(), schemaBase(resolved), &schema); err != nil {
				return err
			}
			content, err := yaml.Marshal(schemaFile{Version: 1, Schema: schema.Schema})
			if err != nil {
				return err
			}
			if outPath == "-" {
				_, err := fmt.Fprint(out.Out, string(content))
				return err
			}
			if err := os.WriteFile(outPath, content, 0o644); err != nil { //nolint:gosec // G306: generated schema file is meant to be committed
				return fmt.Errorf("could not write %s: %w", outPath, err)
			}
			out.Success("Wrote %d schema entries to %s.", len(schema.Schema), outPath)
			return nil
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", defaultSchemaFile, "output file (- for stdout)")
	return cmd
}

func newSchemaPushCommand(opts *Options) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "push [FILE]",
		Short: "Upload the schema from a file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			path := defaultSchemaFile
			if len(args) == 1 {
				path = args[0]
			}
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("could not read %s: %w", path, err)
			}
			var file schemaFile
			if err := yaml.Unmarshal(raw, &file); err != nil {
				return fmt.Errorf("could not parse %s: %w", path, err)
			}
			if file.Version != 0 && file.Version != 1 {
				return fmt.Errorf("unsupported schema file version %d", file.Version)
			}
			if dryRun {
				content, err := yaml.Marshal(schemaFile{Version: 1, Schema: file.Schema})
				if err != nil {
					return err
				}
				_, err = fmt.Fprint(out.Out, string(content))
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result schemaResponse
			if err := c.Put(cmd.Context(), schemaBase(resolved), map[string]any{"schema": file.Schema}, &result); err != nil {
				return err
			}
			out.Success("Pushed %d schema entries.", len(result.Schema))
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate and print the file without uploading")
	return cmd
}

func newSchemaInferCommand(opts *Options) *cobra.Command {
	var outPath string
	var push bool
	cmd := &cobra.Command{
		Use:   "infer",
		Short: "Suggest a schema from the current environment",
		Long: "Read resolved values to suggest types, then print or write a schema file.\n" +
			"The suggestions never include secret values.",
		Args: cobra.NoArgs,
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
			var secrets secretsListResponse
			path := fmt.Sprintf("%s/environments/%s/secrets:resolve?channel=cli",
				projectBase(resolved), url.PathEscape(resolved.Env))
			if err := c.Get(cmd.Context(), path, &secrets); err != nil {
				return err
			}
			suggestions := inferSchema(secrets.Secrets)
			content, err := yaml.Marshal(schemaFile{Version: 1, Schema: suggestions})
			if err != nil {
				return err
			}
			if push {
				var result schemaResponse
				if err := c.Put(cmd.Context(), schemaBase(resolved), map[string]any{"schema": suggestions}, &result); err != nil {
					return err
				}
				out.Success("Inferred and pushed %d schema entries.", len(result.Schema))
				out.Warn("Review inferred types before relying on them in CI.")
				return nil
			}
			if outPath == "" || outPath == "-" {
				_, err := fmt.Fprint(out.Out, string(content))
				return err
			}
			if err := os.WriteFile(outPath, content, 0o644); err != nil { //nolint:gosec // G306: generated schema file is meant to be committed
				return fmt.Errorf("could not write %s: %w", outPath, err)
			}
			out.Success("Wrote %d inferred schema entries to %s.", len(suggestions), outPath)
			out.Warn("Review inferred types before relying on them in CI.")
			return nil
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default stdout)")
	cmd.Flags().BoolVar(&push, "push", false, "upload the inferred schema instead of printing it")
	return cmd
}

func inferSchema(secrets []secretJSON) []schemaJSON {
	out := make([]schemaJSON, 0, len(secrets))
	for _, secret := range secrets {
		kind := secret.Kind
		if kind == "" {
			kind = "secret"
		}
		entry := schemaJSON{Key: secret.Key, Kind: kind, Type: inferType(secret.Value)}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func inferType(value string) string {
	if value == "" {
		return "string"
	}
	trimmed := strings.TrimSpace(value)
	switch strings.ToLower(trimmed) {
	case "true", "false", "yes", "no":
		return "bool"
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return "int"
	}
	if json.Valid([]byte(trimmed)) && (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) {
		return "json"
	}
	if parsed, err := url.Parse(trimmed); err == nil && parsed.Scheme != "" && parsed.Host != "" {
		return "url"
	}
	return "string"
}
