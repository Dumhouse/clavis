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
		identityType      string
		role              string
		oidc              string
		issuer            string
		audience          string
		repo              string
		branch            string
		ghEnvironment     string
		k8sNamespace      string
		k8sServiceAccount string
		gcpServiceAccount string
		awsAccount        string
		awsARN            string
		awsUserID         string
		claims            []string
		project           string
		environment       string
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
			trust, err := buildOIDCTrust(oidcTrustOptions{
				Provider: oidc, Issuer: issuer, Audience: audience, Repo: repo,
				Branch: branch, Environment: ghEnvironment,
				K8sNamespace: k8sNamespace, K8sServiceAccount: k8sServiceAccount,
				GCPServiceAccount: gcpServiceAccount, Claims: claims,
			})
			if err != nil {
				return err
			}
			if trust != nil {
				body["oidc"] = trust
			}
			if awsAccount != "" || awsARN != "" || awsUserID != "" {
				body["aws"] = map[string]string{
					"account": awsAccount, "arn_pattern": awsARN, "user_id_pattern": awsUserID,
				}
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
	cmd.Flags().StringVar(&oidc, "oidc", "", "oidc provider preset: github, gitlab, kubernetes, gcp, or generic")
	cmd.Flags().StringVar(&issuer, "issuer", "", "OIDC issuer URL")
	cmd.Flags().StringVar(&audience, "audience", "", "OIDC audience (default clavis for GitHub/GitLab)")
	cmd.Flags().StringVar(&repo, "repo", "", "repository claim (GitHub owner/name or GitLab project path)")
	cmd.Flags().StringVar(&branch, "branch", "", "branch claim")
	cmd.Flags().StringVar(&ghEnvironment, "gh-environment", "", "GitHub environment claim")
	cmd.Flags().StringVar(&k8sNamespace, "k8s-namespace", "", "Kubernetes namespace for service account auth")
	cmd.Flags().StringVar(&k8sServiceAccount, "k8s-service-account", "", "Kubernetes service account name")
	cmd.Flags().StringVar(&gcpServiceAccount, "gcp-service-account", "", "GCP service account email claim")
	cmd.Flags().StringVar(&awsAccount, "aws-account", "", "AWS account ID for SigV4 trust")
	cmd.Flags().StringVar(&awsARN, "aws-arn", "", "AWS ARN glob for SigV4 trust")
	cmd.Flags().StringVar(&awsUserID, "aws-user-id", "", "AWS user ID glob for SigV4 trust")
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

type oidcTrustOptions struct {
	Provider          string
	Issuer            string
	Audience          string
	Repo              string
	Branch            string
	Environment       string
	K8sNamespace      string
	K8sServiceAccount string
	GCPServiceAccount string
	Claims            []string
}

func buildOIDCTrust(in oidcTrustOptions) (map[string]any, error) {
	if in.Provider == "" {
		return nil, nil
	}
	trust := map[string]any{}
	claimMap := map[string]string{}
	switch in.Provider {
	case "github":
		if in.Issuer == "" {
			in.Issuer = "https://token.actions.githubusercontent.com"
		}
		if in.Audience == "" {
			in.Audience = "clavis"
		}
		if in.Repo != "" {
			claimMap["repository"] = in.Repo
		}
		if in.Branch != "" {
			claimMap["ref"] = normalizeRef(in.Branch)
		}
		if in.Environment != "" {
			claimMap["environment"] = in.Environment
		}
	case "gitlab":
		if in.Issuer == "" {
			in.Issuer = "https://gitlab.com"
		}
		if in.Audience == "" {
			in.Audience = "clavis"
		}
		if in.Repo != "" {
			claimMap["project_path"] = in.Repo
		}
		if in.Branch != "" {
			claimMap["ref"] = normalizeRef(in.Branch)
		}
		if in.Environment != "" {
			claimMap["environment"] = in.Environment
		}
	case "kubernetes":
		if in.Audience == "" {
			in.Audience = "clavis"
		}
		if in.Issuer == "" {
			return nil, fmt.Errorf("--issuer is required for Kubernetes service account auth")
		}
		if (in.K8sNamespace == "") != (in.K8sServiceAccount == "") {
			return nil, fmt.Errorf("--k8s-namespace and --k8s-service-account must be used together")
		}
		if in.K8sNamespace != "" {
			claimMap["sub"] = "system:serviceaccount:" + in.K8sNamespace + ":" + in.K8sServiceAccount
		}
	case "gcp":
		if in.Issuer == "" {
			in.Issuer = "https://accounts.google.com"
		}
		if in.Audience == "" {
			in.Audience = "clavis"
		}
		if in.GCPServiceAccount != "" {
			claimMap["email"] = in.GCPServiceAccount
		}
	case "generic":
	default:
		return nil, fmt.Errorf("--oidc must be github, gitlab, kubernetes, gcp, or generic")
	}
	for _, claim := range in.Claims {
		parts := strings.SplitN(claim, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --claim %q (want key=value)", claim)
		}
		claimMap[parts[0]] = parts[1]
	}
	if len(claimMap) > 0 {
		trust["claims"] = claimMap
	}
	trust["issuer"] = in.Issuer
	trust["audience"] = in.Audience
	return trust, nil
}

func normalizeRef(branch string) string {
	if strings.HasPrefix(branch, "refs/") {
		return branch
	}
	return "refs/heads/" + branch
}
