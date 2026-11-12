package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

type healthResponse struct {
	Score    int `json:"score"`
	Checked  int `json:"checked"`
	Findings []struct {
		Category string `json:"category"`
		Level    string `json:"level"`
		Message  string `json:"message"`
		Project  string `json:"project,omitempty"`
		Env      string `json:"env,omitempty"`
		Key      string `json:"key,omitempty"`
		Subject  string `json:"subject,omitempty"`
	} `json:"findings"`
}

func newHealthCommand(opts *Options) *cobra.Command {
	var project string
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Show organization health findings",
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
			path := "/api/v1/orgs/" + url.PathEscape(resolved.Org) + "/health"
			if project != "" {
				path += "?project=" + url.QueryEscape(project)
			}
			var report healthResponse
			if err := c.Get(cmd.Context(), path, &report); err != nil {
				return err
			}
			if opts.JSON {
				return out.PrintJSON(report)
			}
			out.Info("Health score: %d/100 (%d secrets checked)", report.Score, report.Checked)
			rows := make([][]string, 0, len(report.Findings))
			for _, finding := range report.Findings {
				subject := finding.Key
				if subject == "" {
					subject = finding.Subject
				}
				location := finding.Project
				if finding.Env != "" {
					location += "/" + finding.Env
				}
				rows = append(rows, []string{finding.Level, finding.Category, subject, location, finding.Message})
			}
			out.Table([]string{"LEVEL", "CATEGORY", "RESOURCE", "LOCATION", "MESSAGE"}, rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&project, "project", "", "limit to one project slug")
	return cmd
}
