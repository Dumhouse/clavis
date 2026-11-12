package cli

import (
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type offboardingTaskJSON struct {
	ID          string    `json:"id"`
	ProjectSlug string    `json:"project"`
	EnvSlug     string    `json:"environment"`
	Key         string    `json:"key"`
	Status      string    `json:"status"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type offboardingJSON struct {
	UserID   string                `json:"user_id"`
	Email    string                `json:"email,omitempty"`
	Name     string                `json:"name,omitempty"`
	Done     int                   `json:"done"`
	Total    int                   `json:"total"`
	Exported int                   `json:"exported"`
	Tasks    []offboardingTaskJSON `json:"tasks"`
}

func newMembersOffboardingCommand(opts *Options) *cobra.Command {
	var start bool
	cmd := &cobra.Command{
		Use:   "offboarding USER",
		Short: "Show or start a departing member's secret review",
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
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/members/" + url.PathEscape(args[0]) + "/offboarding"
			var report offboardingJSON
			if start {
				if err := c.Post(cmd.Context(), path, map[string]any{}, &report); err != nil {
					return err
				}
			} else if err := c.Get(cmd.Context(), path, &report); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(report)
			}
			out.Info("%s (%s): %d/%d reviewed, %d exported", report.Email, report.UserID, report.Done, report.Total, report.Exported)
			rows := make([][]string, 0, len(report.Tasks))
			for _, task := range report.Tasks {
				rows = append(rows, []string{task.ID, task.ProjectSlug, task.EnvSlug, task.Key, task.Status})
			}
			out.Table([]string{"TASK", "PROJECT", "ENV", "KEY", "STATUS"}, rows)
			return nil
		},
	}
	cmd.Flags().BoolVar(&start, "start", false, "create tasks from reveal history")
	return cmd
}

func newMembersMarkSafeCommand(opts *Options, status string) *cobra.Command {
	use := "mark-safe USER TASK_ID"
	if status == "rotated" {
		use = "mark-rotated USER TASK_ID"
	}
	return &cobra.Command{
		Use:   use,
		Short: "Mark an offboarding task reviewed",
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
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/members/" + url.PathEscape(args[0]) + "/offboarding/" + url.PathEscape(args[1])
			var report offboardingJSON
			if err := c.Post(cmd.Context(), path, map[string]any{"status": status}, &report); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(report)
			}
			out.Success("Marked task %s %s.", args[1], status)
			return nil
		},
	}
}
