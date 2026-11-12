package cli

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/service"
)

type schemaJSON struct {
	Key         string          `json:"key" yaml:"key"`
	Type        string          `json:"type,omitempty" yaml:"type,omitempty"`
	Kind        string          `json:"kind,omitempty" yaml:"kind,omitempty"`
	Required    map[string]bool `json:"required,omitempty" yaml:"required,omitempty"`
	Pattern     string          `json:"pattern,omitempty" yaml:"pattern,omitempty"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Example     string          `json:"example,omitempty" yaml:"example,omitempty"`
	Enum        []string        `json:"enum,omitempty" yaml:"enum,omitempty"`
}

type schemaResponse struct {
	Schema []schemaJSON `json:"schema" yaml:"schema"`
}

type checkItem struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type environmentCheck struct {
	Environment string      `json:"environment"`
	Type        string      `json:"type"`
	OK          bool        `json:"ok"`
	Checks      []checkItem `json:"checks"`
}

type checkReport struct {
	OK           bool               `json:"ok"`
	Environments []environmentCheck `json:"environments"`
}

func schemaBase(r *resolvedContext) string {
	return projectBase(r) + "/schema"
}

func newCheckCommand(opts *Options) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Validate an environment against the project's secret schema",
		Long: "Validate required keys, types, patterns, and enum values. The report lists\n" +
			"every schema key and exits with code 6 when any check fails, so it can gate CI.",
		Args: cobra.NoArgs,
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
			var project projectDetail
			if err := c.Get(cmd.Context(), projectBase(resolved), &project); err != nil {
				return err
			}
			envs := project.Environments
			if !all {
				if resolved.Env == "" {
					return fmt.Errorf("no environment selected: pass --env or use --all")
				}
				envs = nil
				for _, env := range project.Environments {
					if env.Slug == resolved.Env {
						envs = append(envs, env)
						break
					}
				}
				if len(envs) == 0 {
					return fmt.Errorf("environment %q not found in project %q", resolved.Env, resolved.Project)
				}
			}
			report := checkReport{OK: true}
			for _, env := range envs {
				var secrets secretsListResponse
				path := fmt.Sprintf("%s/environments/%s/secrets:resolve?channel=cli",
					projectBase(resolved), url.PathEscape(env.Slug))
				if err := c.Get(cmd.Context(), path, &secrets); err != nil {
					return err
				}
				result := checkEnvironment(schema.Schema, secrets.Secrets, env)
				if !result.OK {
					report.OK = false
				}
				report.Environments = append(report.Environments, result)
			}
			if opts.JSON {
				if err := out.PrintJSON(report); err != nil {
					return err
				}
			} else {
				renderCheckReport(out, report)
			}
			if !report.OK {
				failures := 0
				for _, env := range report.Environments {
					for _, item := range env.Checks {
						if item.Status == "error" {
							failures++
						}
					}
				}
				return &ExitError{
					Code:   ExitValidation,
					Err:    fmt.Errorf("%d schema check(s) failed", failures),
					Silent: true,
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "check every environment in the project")
	return cmd
}

func checkEnvironment(schema []schemaJSON, secrets []secretJSON, env environmentJSON) environmentCheck {
	result := environmentCheck{Environment: env.Slug, Type: env.Type, OK: true}
	values := make(map[string]secretJSON, len(secrets))
	for _, secret := range secrets {
		values[secret.Key] = secret
	}
	keys := make([]string, 0, len(schema))
	for i := range schema {
		keys = append(keys, schema[i].Key)
	}
	sort.Strings(keys)
	byKey := make(map[string]schemaJSON, len(schema))
	for _, entry := range schema {
		byKey[entry.Key] = entry
	}
	for _, key := range keys {
		entry := byKey[key]
		secret, present := values[key]
		if !present {
			if entry.Required[env.Type] {
				result.OK = false
				result.Checks = append(result.Checks, checkItem{
					Key: key, Status: "error",
					Message: fmt.Sprintf("required in %s but missing", env.Type),
				})
			} else {
				result.Checks = append(result.Checks, checkItem{
					Key: key, Status: "warning", Message: "missing (optional)",
				})
			}
			continue
		}
		if secret.Kind == "file" || entry.Kind == "file" {
			result.Checks = append(result.Checks, checkItem{Key: key, Status: "ok"})
			continue
		}
		serviceEntry := service.SchemaEntry{
			Key: entry.Key, Type: entry.Type, Kind: entry.Kind, Required: entry.Required,
			Pattern: entry.Pattern, Description: entry.Description, Example: entry.Example,
			Enum: entry.Enum,
		}
		if err := serviceEntry.ValidateValue(secret.Value); err != nil {
			result.OK = false
			result.Checks = append(result.Checks, checkItem{
				Key: key, Status: "error", Message: schemaErrorDetail(err),
			})
			continue
		}
		if entry.Kind != "" && secret.Kind != "" && entry.Kind != secret.Kind {
			result.Checks = append(result.Checks, checkItem{
				Key: key, Status: "warning",
				Message: fmt.Sprintf("kind is %s, schema expects %s", secret.Kind, entry.Kind),
			})
			continue
		}
		result.Checks = append(result.Checks, checkItem{Key: key, Status: "ok"})
	}
	// Keys present outside the schema are allowed but worth surfacing.
	known := make(map[string]bool, len(schema))
	for _, entry := range schema {
		known[entry.Key] = true
	}
	for _, secret := range secrets {
		if known[secret.Key] {
			continue
		}
		result.Checks = append(result.Checks, checkItem{
			Key: secret.Key, Status: "warning", Message: "not described by the schema",
		})
	}
	sort.SliceStable(result.Checks, func(i, j int) bool { return result.Checks[i].Key < result.Checks[j].Key })
	return result
}

func schemaErrorDetail(err error) string {
	detail := err.Error()
	detail = strings.TrimPrefix(detail, "service: validation failed: ")
	return detail
}

func renderCheckReport(out *Output, report checkReport) {
	for _, env := range report.Environments {
		if len(report.Environments) > 1 {
			fmt.Fprintf(out.Out, "\n%s (%s)\n", env.Environment, env.Type)
		}
		rows := make([][]string, 0, len(env.Checks))
		for _, item := range env.Checks {
			rows = append(rows, []string{item.Key, strings.ToUpper(item.Status), item.Message})
		}
		out.Table([]string{"KEY", "STATUS", "DETAIL"}, rows)
	}
	if report.OK {
		out.Success("Schema check passed.")
		return
	}
	out.Errorf("Schema check failed.")
}
