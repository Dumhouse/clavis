package cli

import (
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type accessRequestJSON struct {
	ID                string     `json:"id"`
	RequesterID       string     `json:"requester_id"`
	Environment       string     `json:"environment"`
	Role              string     `json:"role"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	DurationSeconds   int        `json:"duration_seconds"`
	RequiredApprovals int        `json:"required_approvals"`
	Approvals         []string   `json:"approvals"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

type accessRequestsResponse struct {
	Requests []accessRequestJSON `json:"requests"`
}

func accessRequestsBase(resolved *resolvedContext) string {
	return projectBase(resolved) + "/access-requests"
}

func newAccessRequestCommand(opts *Options) *cobra.Command {
	var env, role, reason string
	var duration time.Duration
	var approvals int
	cmd := &cobra.Command{
		Use:   "request",
		Short: "Request temporary project/environment access",
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
			if env == "" {
				env = resolved.Env
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var created accessRequestJSON
			if err := c.Post(cmd.Context(), accessRequestsBase(resolved), map[string]any{
				"environment": env, "role": role, "reason": reason,
				"duration_seconds": int(duration.Seconds()), "required_approvals": approvals,
			}, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Requested %s access to %s for %s (%d approval(s) needed).", created.Role, created.Environment, duration, created.RequiredApprovals)
			out.Info("Request ID: %s", created.ID)
			return nil
		},
	}
	cmd.Flags().StringVar(&env, "env", "", "environment slug (default linked env)")
	cmd.Flags().StringVar(&role, "role", "Developer", "role to request")
	cmd.Flags().StringVar(&reason, "reason", "", "why access is needed")
	cmd.Flags().DurationVar(&duration, "duration", time.Hour, "access duration (5m–24h)")
	cmd.Flags().IntVar(&approvals, "approvals", 1, "required approvals")
	return cmd
}

func newAccessRequestsCommand(opts *Options) *cobra.Command {
	var status string
	cmd := &cobra.Command{
		Use:   "requests",
		Short: "List access requests",
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
			var result accessRequestsResponse
			path := accessRequestsBase(resolved) + "?status=" + url.QueryEscape(status)
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Requests)
			}
			rows := make([][]string, 0, len(result.Requests))
			for _, request := range result.Requests {
				rows = append(rows, []string{
					request.ID, request.RequesterID, request.Environment, request.Role,
					fmt.Sprintf("%d/%d", len(request.Approvals), request.RequiredApprovals),
					request.Status, request.CreatedAt.Local().Format(time.DateTime),
				})
			}
			out.Table([]string{"ID", "REQUESTER", "ENV", "ROLE", "APPROVALS", "STATUS", "CREATED"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&status, "status", "pending", "pending, approved, or rejected")
	return cmd
}

func newAccessApproveCommand(opts *Options, approve bool) *cobra.Command {
	use := "approve REQUEST_ID"
	action := "approve"
	if !approve {
		use = "reject REQUEST_ID"
		action = "reject"
	}
	return &cobra.Command{
		Use:   use,
		Short: "Approve or reject an access request",
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
			var result accessRequestJSON
			if err := c.Post(cmd.Context(), accessRequestsBase(resolved)+"/"+url.PathEscape(args[0])+"/"+action, map[string]any{}, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			out.Success("Access request %s is now %s.", result.ID, result.Status)
			return nil
		},
	}
}

func newAccessBreakGlassCommand(opts *Options) *cobra.Command {
	var env, role, reason string
	var duration time.Duration
	cmd := &cobra.Command{
		Use:   "break-glass",
		Short: "Immediately grant break-glass access and lift an active freeze",
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
			if env == "" {
				env = resolved.Env
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var created struct {
				ID        string    `json:"id"`
				Role      string    `json:"role"`
				ExpiresAt time.Time `json:"expires_at"`
			}
			if err := c.Post(cmd.Context(), accessRequestsBase(resolved)+"/break-glass", map[string]any{
				"environment": env, "role": role, "reason": reason,
				"duration_seconds": int(duration.Seconds()),
			}, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Warn("Break-glass access granted and audited.")
			out.Success("%s on %s until %s.", created.Role, env, created.ExpiresAt.Local().Format(time.DateTime))
			return nil
		},
	}
	cmd.Flags().StringVar(&env, "env", "", "environment slug (default linked env)")
	cmd.Flags().StringVar(&role, "role", "Admin", "role to grant")
	cmd.Flags().StringVar(&reason, "reason", "", "incident reason (required)")
	cmd.Flags().DurationVar(&duration, "duration", time.Hour, "access duration (5m–4h)")
	return cmd
}
