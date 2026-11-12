package cli

import (
	"encoding/csv"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func newAccessReviewCommand(opts *Options) *cobra.Command {
	var env, format string
	cmd := &cobra.Command{
		Use:   "review",
		Short: "Report who can read values in an environment",
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
				env = "prod"
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var rows []struct {
				SubjectKind string `json:"subject_kind"`
				SubjectID   string `json:"subject_id"`
				Name        string `json:"name"`
				Email       string `json:"email"`
				Role        string `json:"role"`
			}
			path := fmt.Sprintf("/api/v1/orgs/%s/access-review?project=%s&env=%s",
				url.PathEscape(resolved.Org), url.QueryEscape(resolved.Project), url.QueryEscape(env))
			if err := c.Get(cmd.Context(), path, &rows); err != nil {
				return err
			}
			if format == "csv" {
				writer := csv.NewWriter(out.Out)
				defer writer.Flush()
				_ = writer.Write([]string{"subject_kind", "subject_id", "name", "email", "role"})
				for _, row := range rows {
					_ = writer.Write([]string{row.SubjectKind, row.SubjectID, row.Name, row.Email, row.Role})
				}
				return writer.Error()
			}
			if opts.JSON {
				return out.PrintJSON(rows)
			}
			table := make([][]string, 0, len(rows))
			for _, row := range rows {
				table = append(table, []string{row.SubjectKind, row.Name, row.Email, row.Role})
			}
			out.Table([]string{"KIND", "NAME", "EMAIL", "ROLE"}, table)
			return nil
		},
	}
	cmd.Flags().StringVar(&env, "env", "prod", "environment to review")
	cmd.Flags().StringVar(&format, "format", "table", "table or csv")
	return cmd
}
