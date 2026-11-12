package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type identityJSON struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Type         string `json:"type"`
	Role         string `json:"role"`
	ProjectID    string `json:"project_id,omitempty"`
	Environment  string `json:"environment,omitempty"`
	OIDCIssuer   string `json:"oidc_issuer,omitempty"`
	OIDCAudience string `json:"oidc_audience,omitempty"`
}

type identitiesResponse struct {
	Identities []identityJSON `json:"identities"`
}

type identityCreateResponse struct {
	Identity identityJSON `json:"identity"`
	Token    string       `json:"token"`
}

func newIdentitiesCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "identities",
		Short: "Manage machine and workload identities",
	}
	cmd.AddCommand(newIdentitiesListCommand(opts))
	cmd.AddCommand(newIdentitiesCreateCommand(opts))
	cmd.AddCommand(newIdentitiesRevokeCommand(opts))
	return cmd
}

func newIdentitiesListCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List identities",
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
			if opts.JSON {
				return out.PrintJSON(result.Identities)
			}
			rows := make([][]string, 0, len(result.Identities))
			for _, identity := range result.Identities {
				rows = append(rows, []string{identity.Name, identity.Type, identity.Role, identity.Environment, identity.OIDCIssuer})
			}
			out.Table([]string{"NAME", "TYPE", "ROLE", "ENV", "OIDC ISSUER"}, rows)
			return nil
		},
	}
}

func newIdentitiesCreateCommand(opts *Options) *cobra.Command {
	var (
		identityType  string
		role          string
		oidc          string
		issuer        string
		audience      string
		repo          string
		branch        string
		ghEnvironment string
		claims        []string
		project       string
		environment   string
	)
	cmd := &cobra.Command{
		Use:   "create NAME",
		Short: "Create a machine or workload identity and its first token",
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
			body := map[string]any{
				"name": args[0], "type": identityType, "role": role,
				"project": project, "environment": environment,
			}
			trust := map[string]any{}
			if oidc != "" {
				switch oidc {
				case "github":
					if issuer == "" {
						issuer = "https://token.actions.githubusercontent.com"
					}
					if audience == "" {
						audience = "clavis"
					}
					claimMap := map[string]string{}
					if repo != "" {
						claimMap["repository"] = repo
					}
					if branch != "" {
						if strings.HasPrefix(branch, "refs/") {
							claimMap["ref"] = branch
						} else {
							claimMap["ref"] = "refs/heads/" + branch
						}
					}
					if ghEnvironment != "" {
						claimMap["environment"] = ghEnvironment
					}
					for _, claim := range claims {
						parts := strings.SplitN(claim, "=", 2)
						if len(parts) != 2 {
							return fmt.Errorf("invalid --claim %q (want key=value)", claim)
						}
						claimMap[parts[0]] = parts[1]
					}
					trust["claims"] = claimMap
				case "generic":
					for _, claim := range claims {
						parts := strings.SplitN(claim, "=", 2)
						if len(parts) != 2 {
							return fmt.Errorf("invalid --claim %q (want key=value)", claim)
						}
						if trust["claims"] == nil {
							trust["claims"] = map[string]string{}
						}
						trust["claims"].(map[string]string)[parts[0]] = parts[1]
					}
				default:
					return fmt.Errorf("--oidc must be github or generic")
				}
				trust["issuer"] = issuer
				trust["audience"] = audience
				body["oidc"] = trust
			}
			var result identityCreateResponse
			endpoint := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/identities"
			if err := c.Post(cmd.Context(), endpoint, body, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			out.Success("Created identity %s.", result.Identity.Name)
			if result.Token != "" {
				out.Info("Token (shown once): %s", result.Token)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&identityType, "type", "machine", "machine or agent")
	cmd.Flags().StringVar(&role, "role", "Developer", "role granted to the identity")
	cmd.Flags().StringVar(&oidc, "oidc", "", "oidc provider preset: github or generic")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL")
	cmd.Flags().StringVar(&audience, "audience", "", "OIDC audience (default clavis for GitHub)")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository claim (owner/name)")
	cmd.Flags().StringVar(&branch, "branch", "", "GitHub branch claim")
	cmd.Flags().StringVar(&ghEnvironment, "gh-environment", "", "GitHub environment claim")
	cmd.Flags().StringArrayVar(&claims, "claim", nil, "additional OIDC claim condition key=value")
	cmd.Flags().StringVar(&project, "project", "", "project slug scope")
	cmd.Flags().StringVar(&environment, "identity-env", "", "environment glob scope")
	return cmd
}

func newIdentitiesRevokeCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "revoke NAME",
		Short: "Revoke an identity and its tokens",
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
			var result identitiesResponse
			if err := c.Get(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/identities", &result); err != nil {
				return err
			}
			var id string
			for _, identity := range result.Identities {
				if strings.EqualFold(identity.Name, args[0]) {
					id = identity.ID
					break
				}
			}
			if id == "" {
				return fmt.Errorf("identity %q not found", args[0])
			}
			if err := c.Delete(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/identities/"+url.PathEscape(id)); err != nil {
				return err
			}
			out.Success("Revoked identity %s.", args[0])
			return nil
		},
	}
}
