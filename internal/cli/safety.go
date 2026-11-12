package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// requireProductionConfirmation shows a banner and, for a production
// environment, requires the project slug to be typed (or --yes in CI).
func requireProductionConfirmation(ctx context.Context, opts *Options, c apiClient, resolved *resolvedContext, action string, yes bool) error {
	var detail projectDetail
	if err := c.Get(ctx, projectBase(resolved), &detail); err != nil {
		return err
	}
	var envType string
	for _, env := range detail.Environments {
		if env.Slug == resolved.Env {
			envType = env.Type
			break
		}
	}
	if envType != "production" {
		return nil
	}
	out := opts.output()
	out.Warn("PRODUCTION: %s will change %s/%s/%s", action, resolved.Org, resolved.Project, resolved.Env)
	if yes {
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("refusing to modify production in a non-interactive session; pass --yes to confirm")
	}
	line, err := promptLine(fmt.Sprintf("Type %q to confirm: ", resolved.Project))
	if err != nil {
		return err
	}
	if strings.TrimSpace(line) != resolved.Project {
		return fmt.Errorf("confirmation did not match; nothing changed")
	}
	return nil
}
