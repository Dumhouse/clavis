package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// newPromptCommand prints a short, offline description of the linked
// project for a shell prompt. It never touches the network, so it is safe to
// run on every prompt, and it prints nothing outside a linked folder.
func newPromptCommand(opts *Options) *cobra.Command {
	var shell string
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "Print the linked project and environment for your shell prompt",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if shell != "" {
				snippet, err := promptSnippet(shell)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprint(opts.output().Out, snippet)
				return nil
			}
			cfg, path, err := loadProjectConfig(".")
			if err != nil || path == "" {
				return nil // not a linked folder: print nothing
			}
			_, _ = fmt.Fprintln(opts.output().Out, promptSegment(
				firstNonEmpty(opts.Project, os.Getenv("CLAVIS_PROJECT"), cfg.Project),
				firstNonEmpty(opts.Env, os.Getenv("CLAVIS_ENV"), cfg.Env, "dev"),
			))
			return nil
		},
	}
	cmd.Flags().StringVar(&shell, "shell", "", "print the lines to add to your shell's startup file: bash, zsh or fish")
	return cmd
}

// promptSegment is "web:dev", or "web:PRODUCTION!" where a mistake costs most.
func promptSegment(project, env string) string {
	if project == "" {
		project = "clavis"
	}
	switch strings.ToLower(env) {
	case "prod", "production", "live":
		return project + ":" + strings.ToUpper(env) + "!"
	}
	return project + ":" + env
}

func promptSnippet(shell string) (string, error) {
	switch shell {
	case "bash":
		return "# Add to ~/.bashrc\nPS1='$(clavis prompt 2>/dev/null | sed \"s/.*/[&] /\")'\"$PS1\"\n", nil
	case "zsh":
		return "# Add to ~/.zshrc\nsetopt PROMPT_SUBST\nPROMPT='$(clavis prompt 2>/dev/null | sed \"s/.*/[&] /\")'\"$PROMPT\"\n", nil
	case "fish":
		return "# Add to ~/.config/fish/conf.d/clavis.fish\nfunction clavis_prompt\n    set -l p (clavis prompt 2>/dev/null)\n    test -n \"$p\"; and echo -n \"[$p] \"\nend\nfunctions -c fish_prompt _clavis_orig_prompt\nfunction fish_prompt\n    clavis_prompt\n    _clavis_orig_prompt\nend\n", nil
	}
	return "", fmt.Errorf("unknown shell %q (bash, zsh or fish)", shell)
}
