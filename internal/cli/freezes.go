package cli

import (
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"
)

type freezeJSON struct {
	ID        string    `json:"id"`
	StartsAt  time.Time `json:"starts_at"`
	EndsAt    time.Time `json:"ends_at"`
	Reason    string    `json:"reason"`
	CreatedBy string    `json:"created_by"`
	Active    bool      `json:"active"`
}

type freezesResponse struct {
	Freezes []freezeJSON `json:"freezes"`
}

func freezesBase(resolved *resolvedContext) string {
	return fmt.Sprintf("/api/v1/orgs/%s/projects/%s/environments/%s/freezes",
		url.PathEscape(resolved.Org), url.PathEscape(resolved.Project), url.PathEscape(resolved.Env))
}

func newEnvFreezeCommand(opts *Options) *cobra.Command {
	var reason, start string
	var hours int
	cmd := &cobra.Command{
		Use:   "freeze",
		Short: "Schedule a write freeze for the linked environment",
		Args:  cobra.NoArgs,
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
			body := map[string]any{"reason": reason, "ends_at": time.Now().UTC().Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)}
			if start != "" {
				parsed, err := time.Parse(time.RFC3339, start)
				if err != nil {
					return fmt.Errorf("--start must be RFC3339: %w", err)
				}
				body["starts_at"] = parsed.Format(time.RFC3339)
			}
			var created freezeJSON
			if err := c.Post(cmd.Context(), freezesBase(resolved), body, &created); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(created)
			}
			out.Success("Frozen %s until %s: %s", resolved.Env, created.EndsAt.Local().Format(time.DateTime), created.Reason)
			return nil
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "why the environment is frozen")
	cmd.Flags().IntVar(&hours, "hours", 4, "freeze duration in hours")
	cmd.Flags().StringVar(&start, "start", "", "start time in RFC3339 (default now)")
	return cmd
}

func newEnvFreezesCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "freezes",
		Short: "List freeze windows",
		Args:  cobra.NoArgs,
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
			var result freezesResponse
			if err := c.Get(cmd.Context(), freezesBase(resolved), &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Freezes)
			}
			rows := make([][]string, 0, len(result.Freezes))
			for _, freeze := range result.Freezes {
				active := ""
				if freeze.Active {
					active = "active"
				}
				rows = append(rows, []string{
					freeze.ID, freeze.StartsAt.Local().Format(time.DateTime),
					freeze.EndsAt.Local().Format(time.DateTime), active, freeze.Reason,
				})
			}
			out.Table([]string{"ID", "STARTS", "ENDS", "STATE", "REASON"}, rows)
			return nil
		},
	}
}

func newEnvUnfreezeCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "unfreeze FREEZE_ID",
		Short: "Delete a freeze window",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(true)
			if err != nil {
				return err
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			if err := c.Delete(cmd.Context(), freezesBase(resolved)+"/"+url.PathEscape(args[0])); err != nil {
				return err
			}
			out.Success("Deleted freeze %s.", args[0])
			return nil
		},
	}
}
