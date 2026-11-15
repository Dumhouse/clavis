package cli

import (
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type environmentJSON struct {
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Protected bool   `json:"protected"`
}

type projectDetail struct {
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	Environments []environmentJSON `json:"environments"`
}

func projectBase(r *resolvedContext) string {
	return "/api/v1/orgs/" + url.PathEscape(r.Org) + "/projects/" + url.PathEscape(r.Project)
}

func newEnvCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environments",
	}
	cmd.AddCommand(newEnvListCommand(opts))
	cmd.AddCommand(newEnvCreateCommand(opts))
	cmd.AddCommand(newEnvDiffCommand(opts))
	cmd.AddCommand(newEnvSimilarCommand(opts))
	cmd.AddCommand(newEnvRemoveCommand(opts))
	cmd.AddCommand(newEnvBranchCommand(opts))
	cmd.AddCommand(newEnvFreezeCommand(opts))
	cmd.AddCommand(newEnvFreezesCommand(opts))
	cmd.AddCommand(newEnvUnfreezeCommand(opts))
	return cmd
}

func newEnvListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List environments",
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
			var detail projectDetail
			if err := c.Get(cmd.Context(), projectBase(resolved), &detail); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(detail.Environments)
			}
			rows := make([][]string, 0, len(detail.Environments))
			for _, env := range detail.Environments {
				protected := ""
				if env.Protected {
					protected = "yes"
				}
				rows = append(rows, []string{env.Slug, env.Name, env.Type, protected})
			}
			out.Table([]string{"ENV", "NAME", "TYPE", "PROTECTED"}, rows)
			return nil
		},
	}
}

func newEnvCreateCommand(opts *Options) *cobra.Command {
	var slug, envType string
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create an environment",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			if slug == "" {
				slug = slugify(args[0])
			}
			var env environmentJSON
			body := map[string]string{"name": args[0], "slug": slug, "type": envType}
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/environments", body, &env); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(env)
			}
			out.Success("Created environment %s (%s, %s)", env.Slug, env.Name, env.Type)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "environment slug")
	cmd.Flags().StringVar(&envType, "type", "development", "development, preview, staging, or production")
	return cmd
}

func newEnvDiffCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "diff SOURCE TARGET",
		Short: "Compare key names between two environments",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			source, err := fetchSecrets(cmd, c, resolved, args[0], true)
			if err != nil {
				return err
			}
			target, err := fetchSecrets(cmd, c, resolved, args[1], true)
			if err != nil {
				return err
			}
			keys := map[string]bool{}
			for key := range source {
				keys[key] = true
			}
			for key := range target {
				keys[key] = true
			}
			names := make([]string, 0, len(keys))
			for key := range keys {
				names = append(names, key)
			}
			sort.Strings(names)

			rows := make([][]string, 0, len(names))
			for _, key := range names {
				src, inSrc := source[key]
				dst, inDst := target[key]
				status := "unchanged"
				switch {
				case !inSrc:
					status = "missing in " + args[0]
				case !inDst:
					status = "missing in " + args[1]
				case src != dst:
					status = "changed"
				}
				rows = append(rows, []string{key, status})
			}
			if opts.JSON {
				return out.PrintJSON(rows)
			}
			out.Table([]string{"KEY", "STATUS"}, rows)
			out.Info("Values are fetched (audited) and compared on this computer; they are never printed.")
			return nil
		},
	}
}

func fetchSecrets(cmd *cobra.Command, c apiClient, r *resolvedContext, env string, reveal bool) (map[string]string, error) {
	scoped := *r
	scoped.Env = env
	path := secretsBase(&scoped)
	if reveal {
		path += "?reveal=true&channel=cli"
	}
	var result secretsListResponse
	if err := c.Get(cmd.Context(), path, &result); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(result.Secrets))
	for _, secret := range result.Secrets {
		out[secret.Key] = secret.Value
	}
	return out, nil
}

func newEnvRemoveCommand(opts *Options) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "rm SLUG",
		Aliases: []string{"delete"},
		Short:   "Delete an environment and the secrets in it",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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
			slug := args[0]
			var detail projectDetail
			if err := c.Get(cmd.Context(), projectBase(resolved), &detail); err != nil {
				return err
			}
			var target *environmentJSON
			for i := range detail.Environments {
				if detail.Environments[i].Slug == slug {
					target = &detail.Environments[i]
				}
			}
			if target == nil {
				return fmt.Errorf("environment %q not found in %s/%s", slug, resolved.Org, resolved.Project)
			}
			if len(detail.Environments) <= 1 {
				return fmt.Errorf("a project must keep at least one environment")
			}
			scoped := *resolved
			scoped.Env = slug
			var list secretsListResponse
			count := 0
			if err := c.Get(cmd.Context(), secretsBase(&scoped), &list); err == nil {
				count = len(list.Secrets)
			}
			out.Warn("This deletes %s (%s) from %s/%s together with its %d secret(s), their history and its overrides. It cannot be undone.",
				slug, target.Type, resolved.Org, resolved.Project, count)
			if !yes {
				if !term.IsTerminal(int(os.Stdin.Fd())) {
					return fmt.Errorf("refusing to delete in a non-interactive session; pass --yes to confirm")
				}
				line, err := promptLine(fmt.Sprintf("Type %q to confirm: ", slug))
				if err != nil {
					return err
				}
				if strings.TrimSpace(line) != slug {
					return fmt.Errorf("confirmation did not match; nothing deleted")
				}
			}
			if err := c.Delete(cmd.Context(), projectBase(resolved)+"/environments/"+url.PathEscape(slug)); err != nil {
				return err
			}
			out.Success("Deleted environment %s.", slug)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation (required without a terminal)")
	return cmd
}
