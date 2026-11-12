package cli

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func newShellCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Start a subshell with secrets in the environment",
		Long: "Start an interactive shell with the linked environment's resolved secrets\n" +
			"exported and a colored prompt. Values are not masked; only use this in a\n" +
			"trusted terminal.",
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
			var result secretsListResponse
			if err := c.Get(cmd.Context(), secretsBase(resolved)+":resolve?channel=cli", &result); err != nil {
				return err
			}
			entries, _, cleanup, err := prepareSecretEnv(result.Secrets, false)
			if err != nil {
				return err
			}
			defer cleanup()
			shell := os.Getenv("SHELL")
			if shell == "" {
				shell = "/bin/sh"
			}
			child := exec.CommandContext(cmd.Context(), shell)
			child.Env = append(os.Environ(), entries...)
			child.Env = append(child.Env,
				"CLAVIS_ORG="+resolved.Org,
				"CLAVIS_PROJECT="+resolved.Project,
				"CLAVIS_ENV="+resolved.Env,
				"PS1=[clavis "+resolved.Project+"/"+resolved.Env+"] $ ",
			)
			child.Stdin = os.Stdin
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			out.Warn("Secrets are exported without masking. Exit the subshell to leave the context.")
			if err := child.Run(); err != nil {
				return fmt.Errorf("shell exited: %w", err)
			}
			return nil
		},
	}
	return cmd
}
