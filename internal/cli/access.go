package cli

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type memberJSON struct {
	UserID   string    `json:"user_id"`
	Email    string    `json:"email"`
	Name     string    `json:"name"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

type membersResponse struct {
	Members []memberJSON `json:"members"`
}

type groupJSON struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type groupsResponse struct {
	Groups []groupJSON `json:"groups"`
}

type bindingJSON struct {
	Role        string     `json:"role"`
	Source      string     `json:"source"`
	SubjectKind string     `json:"subject_kind"`
	SubjectID   string     `json:"subject_id"`
	ProjectID   string     `json:"project_id,omitempty"`
	Environment string     `json:"environment,omitempty"`
	Path        string     `json:"path,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Grants      bool       `json:"grants"`
}

type explainJSON struct {
	Allowed   bool          `json:"allowed"`
	Reason    string        `json:"reason"`
	Subject   string        `json:"subject"`
	ProjectID string        `json:"project_id,omitempty"`
	Matched   *bindingJSON  `json:"matched,omitempty"`
	Bindings  []bindingJSON `json:"bindings"`
}

func newMembersCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "members",
		Short: "Inspect organization members",
	}
	cmd.AddCommand(newMembersListCommand(opts))
	return cmd
}

func newMembersListCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List organization members",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization context: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var result membersResponse
			if err := c.Get(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/members", &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Members)
			}
			if len(result.Members) == 0 {
				out.Info("No members in %s.", resolved.Org)
				return nil
			}
			rows := make([][]string, 0, len(result.Members))
			for _, member := range result.Members {
				rows = append(rows, []string{member.Email, member.Name, member.Role})
			}
			out.Table([]string{"EMAIL", "NAME", "ROLE"}, rows)
			return nil
		},
	}
	return cmd
}

func newAccessCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Explain effective access",
	}
	cmd.AddCommand(newAccessExplainCommand(opts))
	return cmd
}

func newAccessExplainCommand(opts *Options) *cobra.Command {
	var (
		user        string
		group       string
		permission  string
		project     string
		environment string
		path        string
	)
	cmd := &cobra.Command{
		Use:   "explain <user|group>",
		Short: "Explain why a user or group has access",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			if permission == "" {
				return fmt.Errorf("--permission is required (for example secrets:write)")
			}
			if len(args) == 1 {
				if group != "" {
					group = args[0]
				} else {
					user = args[0]
				}
			}
			if (user == "") == (group == "") {
				return fmt.Errorf("pass exactly one of <user>, --user, or --group")
			}
			if environment == "" {
				environment = opts.Env
			}
			if project == "" {
				project = opts.Project
			}
			if path == "" {
				path = opts.Path
			}
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" {
				return fmt.Errorf("no organization context: pass --org or run `clavis init`")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			kind, subjectID := "user", ""
			if user != "" {
				var members membersResponse
				if err := c.Get(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/members", &members); err != nil {
					return err
				}
				subjectID = matchMember(members.Members, user)
				if subjectID == "" {
					return fmt.Errorf("no member matches %q", user)
				}
			} else {
				kind = "group"
				var groups groupsResponse
				if err := c.Get(cmd.Context(), "/api/v1/orgs/"+url.PathEscape(resolved.Org)+"/groups", &groups); err != nil {
					return err
				}
				subjectID = matchGroup(groups.Groups, group)
				if subjectID == "" {
					return fmt.Errorf("no group matches %q", group)
				}
			}
			query := url.Values{}
			query.Set("subject_kind", kind)
			query.Set("subject_id", subjectID)
			query.Set("permission", permission)
			if project != "" {
				query.Set("project", project)
			}
			if environment != "" {
				query.Set("environment", environment)
			}
			if path != "" {
				query.Set("path", path)
			}
			var result explainJSON
			endpoint := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/access/explain?" + query.Encode()
			if err := c.Get(cmd.Context(), endpoint, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			if result.Allowed {
				out.Success("%s can %s", result.Subject, permission)
			} else {
				out.Info("%s cannot %s", result.Subject, permission)
			}
			out.Info("reason: %s", result.Reason)
			if result.Matched != nil {
				out.Info("granted by %s (%s) · project %s · environment %s · path %s",
					result.Matched.Role, result.Matched.Source,
					scopeString(result.Matched.ProjectID, result.Matched.Environment),
					result.Matched.Environment, result.Matched.Path)
			}
			out.Info("bindings:")
			rows := make([][]string, 0, len(result.Bindings))
			for _, binding := range result.Bindings {
				grants := "no"
				if binding.Grants {
					grants = "yes"
				}
				rows = append(rows, []string{binding.Role, binding.Source, binding.ProjectID, binding.Environment, binding.Path, grants})
			}
			out.Table([]string{"ROLE", "SOURCE", "PROJECT", "ENVIRONMENT", "PATH", "GRANTS"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&user, "user", "", "member email, name, or user ID")
	cmd.Flags().StringVar(&group, "group", "", "group name or ID")
	cmd.Flags().StringVar(&permission, "permission", "", "permission to explain, e.g. secrets:write")
	cmd.Flags().StringVar(&project, "project", "", "project slug (default: all projects)")
	cmd.Flags().StringVar(&environment, "environment", "", "environment glob (default: * or --env)")
	cmd.Flags().StringVar(&path, "path", "", "folder path (default: /)")
	return cmd
}

func matchMember(members []memberJSON, needle string) string {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, member := range members {
		if strings.ToLower(member.Email) == needle || strings.ToLower(member.Name) == needle || member.UserID == needle {
			return member.UserID
		}
	}
	return ""
}

func matchGroup(groups []groupJSON, needle string) string {
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, group := range groups {
		if strings.ToLower(group.Name) == needle || group.ID == needle {
			return group.ID
		}
	}
	return ""
}

func scopeString(projectID, environment string) string {
	project := projectID
	if project == "" {
		project = "all projects"
	}
	if environment == "" {
		environment = "*"
	}
	return project + "/" + environment
}
