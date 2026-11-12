package cli

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

func newEnvBranchCommand(opts *Options) *cobra.Command {
	var branch string
	cmd := &cobra.Command{
		Use:   "branch [BRANCH]",
		Short: "Create or refresh a preview environment for a git branch",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			if len(args) == 1 {
				branch = args[0]
			}
			if branch == "" {
				raw, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
				if err != nil {
					return fmt.Errorf("could not detect the git branch: %w", err)
				}
				branch = strings.TrimSpace(string(raw))
			}
			if branch == "" || branch == "HEAD" {
				return fmt.Errorf("pass a branch name, or run this inside a git branch")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var env environmentJSON
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/environments:branch", map[string]string{
				"branch": branch,
			}, &env); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(env)
			}
			out.Success("Branch environment %s is ready for %s.", env.Slug, branch)
			out.Info("Use it with: clavis --env %s <command>", env.Slug)
			return nil
		},
	}
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "git branch (default: current branch)")
	return cmd
}
