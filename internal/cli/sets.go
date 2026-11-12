package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

type sharedSetJSON struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Name  string `json:"name"`
	Items []struct {
		Key     string `json:"key"`
		Kind    string `json:"kind"`
		Value   string `json:"value,omitempty"`
		Version int    `json:"version"`
	} `json:"items"`
}

type sharedSetsResponse struct {
	Sets []sharedSetJSON `json:"sets"`
}

func newSetsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{Use: "sets", Short: "Manage org-level shared secret sets"}
	cmd.AddCommand(newSetsListCommand(opts))
	cmd.AddCommand(newSetsShowCommand(opts))
	cmd.AddCommand(newSetsCreateCommand(opts))
	cmd.AddCommand(newSetsSetCommand(opts))
	cmd.AddCommand(newSetsRemoveCommand(opts))
	cmd.AddCommand(newSetsImportCommand(opts, false))
	cmd.AddCommand(newSetsImportCommand(opts, true))
	return cmd
}

func setsBase(org string) string {
	return "/api/v1/orgs/" + url.PathEscape(org) + "/sets"
}

func newSetsListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List shared sets",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result sharedSetsResponse
			if err := c.Get(cmd.Context(), setsBase(resolved.Org), &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Sets)
			}
			rows := make([][]string, 0, len(result.Sets))
			for _, set := range result.Sets {
				rows = append(rows, []string{set.Slug, set.Name, fmt.Sprintf("%d", len(set.Items))})
			}
			out.Table([]string{"SLUG", "NAME", "KEYS"}, rows)
			return nil
		},
	}
}

func newSetsShowCommand(opts *Options) *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "show SET",
		Short: "Show a shared set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			path := setsBase(resolved.Org) + "/" + url.PathEscape(args[0])
			if reveal {
				path += "?reveal=true"
			}
			var set sharedSetJSON
			if err := c.Get(cmd.Context(), path, &set); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(set)
			}
			rows := make([][]string, 0, len(set.Items))
			for _, item := range set.Items {
				rows = append(rows, []string{item.Key, item.Kind, fmt.Sprintf("%d", item.Version), item.Value})
			}
			out.Table([]string{"KEY", "KIND", "VERSION", "VALUE"}, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "decrypt values (requires secrets:read-values)")
	return cmd
}

func newSetsCreateCommand(opts *Options) *cobra.Command {
	var slug string
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a shared set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var set sharedSetJSON
			if err := c.Post(cmd.Context(), setsBase(resolved.Org), map[string]string{"name": args[0], "slug": slug}, &set); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(set)
			}
			out.Success("Created shared set %s.", set.Slug)
			return nil
		},
	}
	cmd.Flags().StringVar(&slug, "slug", "", "set slug")
	return cmd
}

func newSetsSetCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "set SET KEY [VALUE]",
		Short: "Set a shared set value",
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			value := ""
			if len(args) == 3 {
				value = args[2]
			} else {
				value, err = promptSecret("Value for " + args[1] + ": ")
				if err != nil {
					return err
				}
			}
			path := setsBase(resolved.Org) + "/" + url.PathEscape(args[0]) + "/items/" + url.PathEscape(args[1])
			if err := c.Put(cmd.Context(), path, map[string]string{"value": value}, nil); err != nil {
				return err
			}
			out.Success("Set %s in %s.", args[1], args[0])
			return nil
		},
	}
}

func newSetsRemoveCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rm SET KEY",
		Short: "Remove a shared set value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			path := setsBase(resolved.Org) + "/" + url.PathEscape(args[0]) + "/items/" + url.PathEscape(args[1])
			if err := c.Delete(cmd.Context(), path); err != nil {
				return err
			}
			out.Success("Removed %s from %s.", args[1], args[0])
			return nil
		},
	}
}

func newSetsImportCommand(opts *Options, detach bool) *cobra.Command {
	var environment string
	use := "import SET"
	short := "Import a shared set into an environment"
	if detach {
		use = "detach SET"
		short = "Detach a shared set from an environment"
	}
	cmd := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			env := firstNonEmpty(environment, resolved.Env)
			endpoint := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/projects/" + url.PathEscape(resolved.Project) +
				"/environments/" + url.PathEscape(env) + "/sets/" + url.PathEscape(args[0])
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if detach {
				if err := c.Delete(cmd.Context(), endpoint); err != nil {
					return err
				}
				out.Success("Detached %s from %s.", args[0], env)
				return nil
			}
			if err := c.Post(cmd.Context(), endpoint, map[string]string{}, nil); err != nil {
				return err
			}
			out.Success("Imported %s into %s.", args[0], env)
			return nil
		},
	}
	cmd.Flags().StringVar(&environment, "env", "", "environment slug (default: linked environment)")
	return cmd
}
