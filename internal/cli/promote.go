package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dumhouse/clavis/pkg/problem"
)

type promotionEntryJSON struct {
	Key         string `json:"key"`
	Status      string `json:"status"`
	Kind        string `json:"kind,omitempty"`
	SchemaError string `json:"schema_error,omitempty"`
}

type promotionPlanJSON struct {
	Source           string               `json:"source"`
	Target           string               `json:"target"`
	SourceType       string               `json:"source_type"`
	TargetType       string               `json:"target_type"`
	TargetProtected  bool                 `json:"target_protected"`
	ApprovalRequired bool                 `json:"approval_required"`
	Entries          []promotionEntryJSON `json:"entries"`
}

type promotionResultJSON struct {
	Applied    int    `json:"applied"`
	SnapshotID string `json:"snapshot_id,omitempty"`
}

func newPromoteCommand(opts *Options) *cobra.Command {
	var source, target, keysFlag, message string
	var all, interactive, snapshot, request, yes bool
	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Promote selected keys between environments",
		Long: "Diff two environments, select keys, validate them against the schema, and\n" +
			"copy them to the target. Protected targets need an approval request unless\n" +
			"you are an Admin or Owner.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			resolved, err := opts.resolveContext(false)
			if err != nil {
				return err
			}
			if resolved.Org == "" || resolved.Project == "" {
				return fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
			}
			if source == "" {
				source = resolved.Env
			}
			if source == "" || target == "" {
				return fmt.Errorf("pass --from SOURCE and --to TARGET")
			}
			c, err := resolved.newClient()
			if err != nil {
				return err
			}
			var plan promotionPlanJSON
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/promotions:plan", map[string]any{
				"source": source, "target": target,
			}, &plan); err != nil {
				return err
			}
			if opts.JSON && !all && keysFlag == "" && !interactive {
				return out.PrintJSON(plan)
			}
			for _, entry := range plan.Entries {
				if entry.SchemaError != "" && (entry.Status == "added" || entry.Status == "changed") {
					out.Warn("%s: %s", entry.Key, entry.SchemaError)
				}
			}
			selected, err := selectPromotionKeys(plan, keysFlag, all, interactive, out)
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				return fmt.Errorf("no keys selected")
			}
			if !opts.JSON {
				rows := make([][]string, 0, len(selected))
				for _, key := range selected {
					status := "selected"
					for _, entry := range plan.Entries {
						if entry.Key == key {
							status = entry.Status
							break
						}
					}
					rows = append(rows, []string{key, status})
				}
				out.Table([]string{"KEY", "STATUS"}, rows)
			}
			targetResolved := *resolved
			targetResolved.Env = target
			if err := requireProductionConfirmation(cmd.Context(), opts, c, &targetResolved, "promote "+source+" -> "+target, yes); err != nil {
				return err
			}
			body := map[string]any{
				"source": source, "target": target, "keys": selected,
				"snapshot": snapshot, "message": message, "request": request,
			}
			var result promotionResultJSON
			if err := c.Post(cmd.Context(), projectBase(resolved)+"/promotions", body, &result); err != nil {
				var p *problem.Problem
				if errors.As(err, &p) && p.Code == "promotion_approval_required" {
					out.Warn("The target is protected. Re-run with --request to ask an Admin or Owner for approval.")
				}
				return err
			}
			if opts.JSON {
				return out.PrintJSON(result)
			}
			if request {
				out.Success("Requested promotion of %d keys from %s to %s.", len(selected), source, target)
				out.Info("An Admin or Owner can approve it in the web UI or with `clavis promote --request`.")
				return nil
			}
			if result.SnapshotID != "" {
				out.Info("Snapshot captured before promotion: %s", result.SnapshotID)
			}
			out.Success("Promoted %d keys from %s to %s.", result.Applied, source, target)
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "from", "", "source environment (default: linked env)")
	cmd.Flags().StringVar(&target, "to", "", "target environment")
	cmd.Flags().StringVar(&keysFlag, "keys", "", "comma-separated keys to promote")
	cmd.Flags().BoolVar(&all, "all", false, "promote every added and changed key")
	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "choose keys interactively")
	cmd.Flags().BoolVar(&snapshot, "snapshot", true, "snapshot the target before writing")
	cmd.Flags().BoolVar(&request, "request", false, "create an approval request instead of writing")
	cmd.Flags().StringVar(&message, "message", "", "change message recorded in history")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip production confirmation")
	return cmd
}

func selectPromotionKeys(plan promotionPlanJSON, keysFlag string, all, interactive bool, out *Output) ([]string, error) {
	if keysFlag != "" {
		parts := strings.FieldsFunc(keysFlag, func(r rune) bool { return r == ',' || r == ' ' })
		outKeys := make([]string, 0, len(parts))
		for _, key := range parts {
			if strings.TrimSpace(key) != "" {
				outKeys = append(outKeys, strings.TrimSpace(key))
			}
		}
		return outKeys, nil
	}
	if all {
		return defaultPromotionKeys(plan), nil
	}
	if !interactive || !term.IsTerminal(int(os.Stdin.Fd())) {
		return defaultPromotionKeys(plan), nil
	}
	for index, entry := range plan.Entries {
		fmt.Fprintf(out.Out, "%3d  %-10s %s\n", index+1, entry.Status, entry.Key)
	}
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(out.Out, "Select keys (comma/space separated, Enter = added+changed): ")
	line, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultPromotionKeys(plan), nil
	}
	selected := make([]string, 0)
	for _, token := range strings.FieldsFunc(line, func(r rune) bool { return r == ',' || r == ' ' }) {
		index, err := strconv.Atoi(token)
		if err != nil || index < 1 || index > len(plan.Entries) {
			return nil, fmt.Errorf("invalid selection %q", token)
		}
		selected = append(selected, plan.Entries[index-1].Key)
	}
	return selected, nil
}

func defaultPromotionKeys(plan promotionPlanJSON) []string {
	keys := make([]string, 0)
	for _, entry := range plan.Entries {
		if entry.Status == "added" || entry.Status == "changed" {
			keys = append(keys, entry.Key)
		}
	}
	sort.Strings(keys)
	return keys
}
