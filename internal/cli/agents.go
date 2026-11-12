package cli

import (
	"fmt"
	"net/url"
	"sort"

	"github.com/spf13/cobra"
)

type agentIdentityJSON struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	AllowedCommands []string `json:"allowed_commands"`
}

type agentAllowlistJSON struct {
	IdentityID string   `json:"identity_id"`
	Name       string   `json:"name"`
	Commands   []string `json:"commands"`
}

func newAgentsCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage AI agent identities and command allowlists",
		Long: "Agent identities can only discover key names and use them for explicitly\n" +
			"allowed commands. The allowlist is server-side and deny-all by default.",
	}
	cmd.AddCommand(newAgentsListCommand(opts))
	cmd.AddCommand(newAgentsAllowCommand(opts))
	cmd.AddCommand(newAgentsRevokeCommand(opts))
	return cmd
}

func newAgentsListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List agent identities",
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
			var result identitiesResponse
			if err := c.Get(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/identities", &result); err != nil {
				return err
			}
			agents := make([]agentIdentityJSON, 0)
			for _, identity := range result.Identities {
				if identity.Type != "agent" {
					continue
				}
				agents = append(agents, agentIdentityJSON{
					ID: identity.ID, Name: identity.Name, Type: identity.Type,
					AllowedCommands: identity.AllowedCommands,
				})
			}
			if opts.JSON {
				return out.PrintJSON(agents)
			}
			if len(agents) == 0 {
				out.Info("No agent identities. Create one with `clavis identities create NAME --type agent`.")
				return nil
			}
			rows := make([][]string, 0, len(agents))
			for _, agent := range agents {
				commands := "(deny all)"
				if len(agent.AllowedCommands) > 0 {
					commands = fmt.Sprintf("%d pattern(s)", len(agent.AllowedCommands))
				}
				rows = append(rows, []string{agent.Name, agent.ID, commands})
			}
			out.Table([]string{"NAME", "ID", "ALLOWLIST"}, rows)
			return nil
		},
	}
}

func newAgentsAllowCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "allow IDENTITY COMMAND...",
		Short: "Add command patterns to an agent allowlist",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return updateAgentAllowlist(opts, cmd, args[0], args[1:], false)
		},
	}
}

func newAgentsRevokeCommand(opts *Options) *cobra.Command {
	var clearAll bool
	cmd := &cobra.Command{
		Use:   "revoke IDENTITY [COMMAND...]",
		Short: "Remove command patterns from an agent allowlist",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			remove := args[1:]
			if clearAll {
				if len(remove) > 0 {
					return fmt.Errorf("pass either command patterns or --clearAll, not both")
				}
				remove = nil
			} else if len(remove) == 0 {
				return fmt.Errorf("pass at least one command pattern, or --clearAll to deny all")
			}
			return updateAgentAllowlist(opts, cmd, args[0], remove, true)
		},
	}
	cmd.Flags().BoolVar(&clearAll, "clear", false, "remove every command pattern (deny all)")
	return cmd
}

func updateAgentAllowlist(opts *Options, cmd *cobra.Command, identityRef string, commands []string, remove bool) error {
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
	base := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/identities"
	var identities identitiesResponse
	if err := c.Get(cmd.Context(), base, &identities); err != nil {
		return err
	}
	identityID := ""
	for _, identity := range identities.Identities {
		if identity.ID == identityRef || identity.Name == identityRef {
			if identity.Type != "agent" {
				return fmt.Errorf("%s is a %s identity, not an agent", identity.Name, identity.Type)
			}
			identityID = identity.ID
			break
		}
	}
	if identityID == "" {
		return fmt.Errorf("agent %q not found", identityRef)
	}
	var current agentAllowlistJSON
	if err := c.Get(cmd.Context(), base+"/"+url.PathEscape(identityID)+"/commands", &current); err != nil {
		return err
	}
	next := mergeCommands(current.Commands, commands, remove)
	var updated agentAllowlistJSON
	if err := c.Put(cmd.Context(), base+"/"+url.PathEscape(identityID)+"/commands", map[string]any{
		"commands": next,
	}, &updated); err != nil {
		return err
	}
	if opts.JSON {
		return out.PrintJSON(updated)
	}
	if len(updated.Commands) == 0 {
		out.Success("Agent %s now denies every command.", updated.Name)
		return nil
	}
	out.Success("Agent %s allows %d command pattern(s).", updated.Name, len(updated.Commands))
	for _, command := range updated.Commands {
		out.Info("  %s", command)
	}
	return nil
}

func mergeCommands(current, changes []string, remove bool) []string {
	set := make(map[string]bool, len(current)+len(changes))
	for _, command := range current {
		set[command] = true
	}
	for _, command := range changes {
		if remove {
			delete(set, command)
		} else {
			set[command] = true
		}
	}
	out := make([]string, 0, len(set))
	for command := range set {
		out = append(out, command)
	}
	sort.Strings(out)
	return out
}
