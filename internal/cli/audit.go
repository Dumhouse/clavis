package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/spf13/cobra"
)

type auditEventJSON struct {
	ID        string         `json:"id"`
	Seq       int64          `json:"seq"`
	Actor     auditActorJSON `json:"actor"`
	Action    string         `json:"action"`
	Resource  auditResJSON   `json:"resource"`
	CreatedAt time.Time      `json:"created_at"`
}

type auditActorJSON struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
}

type auditResJSON struct {
	Type string `json:"type"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type auditListResponse struct {
	Events []auditEventJSON `json:"events"`
}

func newAuditCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Read the organization audit log",
	}
	cmd.AddCommand(newAuditListCommand(opts))
	return cmd
}

func newAuditListCommand(opts *Options) *cobra.Command {
	var limit int
	var after int64
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List recent audit events",
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
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/audit-events?limit=" + strconv.Itoa(limit)
			if after > 0 {
				path += "&after=" + strconv.FormatInt(after, 10)
			}
			var result auditListResponse
			if err := c.Get(cmd.Context(), path, &result); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result.Events)
			}
			rows := make([][]string, 0, len(result.Events))
			for _, event := range result.Events {
				actor := event.Actor.Name
				if actor == "" {
					actor = event.Actor.ID
				}
				resource := event.Resource.Type
				if event.Resource.Name != "" {
					resource += " " + event.Resource.Name
				}
				rows = append(rows, []string{
					strconv.FormatInt(event.Seq, 10),
					event.CreatedAt.Local().Format("2006-01-02 15:04:05"),
					actor,
					event.Action,
					resource,
				})
			}
			out.Table([]string{"SEQ", "TIME", "ACTOR", "ACTION", "RESOURCE"}, rows)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 50, "number of events (1-200)")
	cmd.Flags().Int64Var(&after, "after", 0, "only events after this sequence number")
	return cmd
}
