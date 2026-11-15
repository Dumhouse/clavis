package cli

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/pkg/problem"
)

// minDuplicateLen is the shortest secret value worth comparing: "true",
// "3000" and "dev" repeat everywhere and mean nothing.
const minDuplicateLen = 6

// envSecrets is one environment's own secrets (inherited ones are left out,
// since they repeat the parent's by design).
type envSecrets struct {
	Env     string
	Type    string
	Secrets []secretJSON
}

// sameValueGroup is a set of keys in one environment that hold one value.
type sameValueGroup struct {
	Env  string   `json:"environment"`
	Keys []string `json:"keys"`
}

// sharedKeyGroup is one key whose value is identical in several environments.
type sharedKeyGroup struct {
	Key          string   `json:"key"`
	Environments []string `json:"environments"`
	// WithProduction is true when a production environment shares the value
	// with a non-production one: a leak in dev would expose production.
	WithProduction bool `json:"shared_with_production"`
}

type duplicateReport struct {
	SameValue []sameValueGroup `json:"same_value_different_keys"`
	SharedKey []sharedKeyGroup `json:"same_value_across_environments"`
}

// valueDigest groups values without keeping them in a printable form.
func valueDigest(value string) [32]byte { return sha256.Sum256([]byte(value)) }

// findDuplicates reports repeated secret values. Only kind "secret" values of
// at least minDuplicateLen characters count; settings like "true" do not.
// The report names keys and environments, never values.
func findDuplicates(envs []envSecrets) duplicateReport {
	var report duplicateReport
	type seen struct {
		envs map[string]bool
	}
	across := map[string]map[[32]byte]*seen{} // key -> digest -> environments
	prod := map[string]bool{}
	for _, env := range envs {
		prod[env.Env] = env.Type == "production"
		byValue := map[[32]byte][]string{}
		for _, secret := range env.Secrets {
			if secret.Inherited || secret.Kind != "secret" || len(secret.Value) < minDuplicateLen {
				continue
			}
			digest := valueDigest(secret.Value)
			byValue[digest] = append(byValue[digest], secret.Key)
			if across[secret.Key] == nil {
				across[secret.Key] = map[[32]byte]*seen{}
			}
			if across[secret.Key][digest] == nil {
				across[secret.Key][digest] = &seen{envs: map[string]bool{}}
			}
			across[secret.Key][digest].envs[env.Env] = true
		}
		for _, keys := range byValue {
			if len(keys) > 1 {
				sort.Strings(keys)
				report.SameValue = append(report.SameValue, sameValueGroup{Env: env.Env, Keys: keys})
			}
		}
	}
	for key, digests := range across {
		for _, s := range digests {
			if len(s.envs) < 2 {
				continue
			}
			group := sharedKeyGroup{Key: key}
			for env := range s.envs {
				group.Environments = append(group.Environments, env)
				if prod[env] {
					group.WithProduction = true
				}
			}
			sort.Strings(group.Environments)
			report.SharedKey = append(report.SharedKey, group)
		}
	}
	sort.Slice(report.SameValue, func(i, j int) bool {
		if report.SameValue[i].Env != report.SameValue[j].Env {
			return report.SameValue[i].Env < report.SameValue[j].Env
		}
		return report.SameValue[i].Keys[0] < report.SameValue[j].Keys[0]
	})
	sort.Slice(report.SharedKey, func(i, j int) bool {
		if report.SharedKey[i].WithProduction != report.SharedKey[j].WithProduction {
			return report.SharedKey[i].WithProduction
		}
		return report.SharedKey[i].Key < report.SharedKey[j].Key
	})
	return report
}

// envSimilarity compares two environments by their own keys and values.
type envSimilarity struct {
	A          string `json:"a"`
	B          string `json:"b"`
	CommonKeys int    `json:"common_keys"`
	SameValues int    `json:"same_values"`
	OnlyA      int    `json:"only_a"`
	OnlyB      int    `json:"only_b"`
	// KeyPercent is the share of all keys that both environments have;
	// ValuePercent is the share of common keys with an identical value.
	KeyPercent   int `json:"key_percent"`
	ValuePercent int `json:"value_percent"`
}

func similarEnvironments(envs []envSecrets) []envSimilarity {
	own := make([]map[string]string, len(envs))
	for i, env := range envs {
		own[i] = map[string]string{}
		for _, secret := range env.Secrets {
			if !secret.Inherited {
				own[i][secret.Key] = secret.Value
			}
		}
	}
	var out []envSimilarity
	for i := 0; i < len(envs); i++ {
		for j := i + 1; j < len(envs); j++ {
			s := envSimilarity{A: envs[i].Env, B: envs[j].Env}
			for key, value := range own[i] {
				other, ok := own[j][key]
				switch {
				case !ok:
					s.OnlyA++
				case other == value:
					s.CommonKeys++
					s.SameValues++
				default:
					s.CommonKeys++
				}
			}
			for key := range own[j] {
				if _, ok := own[i][key]; !ok {
					s.OnlyB++
				}
			}
			if all := s.CommonKeys + s.OnlyA + s.OnlyB; all > 0 {
				s.KeyPercent = s.CommonKeys * 100 / all
			}
			if s.CommonKeys > 0 {
				s.ValuePercent = s.SameValues * 100 / s.CommonKeys
			}
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ValuePercent != out[j].ValuePercent {
			return out[i].ValuePercent > out[j].ValuePercent
		}
		return out[i].KeyPercent > out[j].KeyPercent
	})
	return out
}

// loadProjectSecrets reads every environment's own secrets with values, for
// local comparison. Reading values is audited like any reveal.
func loadProjectSecrets(cmd *cobra.Command, opts *Options, only string) (*resolvedContext, []envSecrets, error) {
	resolved, err := opts.resolveContext(false)
	if err != nil {
		return nil, nil, err
	}
	if resolved.Org == "" || resolved.Project == "" {
		return nil, nil, fmt.Errorf("no project context: run `clavis init` or pass --org and --project")
	}
	c, err := resolved.newClient()
	if err != nil {
		return nil, nil, err
	}
	var detail projectDetail
	if err := c.Get(cmd.Context(), projectBase(resolved), &detail); err != nil {
		var p *problem.Problem
		if !errors.As(err, &p) || p.Status != http.StatusForbidden {
			return nil, nil, err
		}
		// A token that cannot read project details (a CI token) can still
		// be checked inside the one environment it is allowed to read.
		opts.output().Warn("This token cannot list the project's environments; checking only %s.", resolved.Env)
		detail.Environments = []environmentJSON{{Slug: resolved.Env}}
	}
	var all []envSecrets
	for _, env := range detail.Environments {
		if only != "" && env.Slug != only {
			continue
		}
		scoped := *resolved
		scoped.Env = env.Slug
		var list secretsListResponse
		if err := c.Get(cmd.Context(), secretsBase(&scoped)+"?reveal=true&channel=cli", &list); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", env.Slug, err)
		}
		all = append(all, envSecrets{Env: env.Slug, Type: env.Type, Secrets: list.Secrets})
	}
	if only != "" && len(all) == 0 {
		return nil, nil, fmt.Errorf("environment %q not found in this project", only)
	}
	return resolved, all, nil
}

func newSecretsDuplicatesCommand(opts *Options) *cobra.Command {
	var envFlag string
	cmd := &cobra.Command{
		Use:   "duplicates",
		Short: "Find secrets that share a value, in one environment or across environments",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			_, envs, err := loadProjectSecrets(cmd, opts, envFlag)
			if err != nil {
				return err
			}
			report := findDuplicates(envs)
			if opts.JSON {
				return out.PrintJSON(report)
			}
			if len(report.SameValue) == 0 && len(report.SharedKey) == 0 {
				out.Success("No repeated secret values found.")
				return nil
			}
			if len(report.SameValue) > 0 {
				rows := make([][]string, 0, len(report.SameValue))
				for _, group := range report.SameValue {
					rows = append(rows, []string{group.Env, strings.Join(group.Keys, ", ")})
				}
				_, _ = fmt.Fprintln(out.Out, "SAME VALUE UNDER DIFFERENT KEYS")
				out.Table([]string{"ENVIRONMENT", "KEYS"}, rows)
			}
			if len(report.SharedKey) > 0 {
				rows := make([][]string, 0, len(report.SharedKey))
				for _, group := range report.SharedKey {
					note := ""
					if group.WithProduction {
						note = "shared with production"
					}
					rows = append(rows, []string{group.Key, strings.Join(group.Environments, ", "), note})
				}
				if len(report.SameValue) > 0 {
					_, _ = fmt.Fprintln(out.Out)
				}
				_, _ = fmt.Fprintln(out.Out, "SAME KEY, SAME VALUE IN SEVERAL ENVIRONMENTS")
				out.Table([]string{"KEY", "ENVIRONMENTS", ""}, rows)
			}
			out.Info("Values are compared on this computer and never printed. Reading them is audited.")
			return nil
		},
	}
	cmd.Flags().StringVar(&envFlag, "only-env", "", "look only inside this environment (skips the cross-environment check)")
	return cmd
}

func newEnvSimilarCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "similar",
		Short: "Compare every pair of environments and show how alike they are",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			_, envs, err := loadProjectSecrets(cmd, opts, "")
			if err != nil {
				return err
			}
			pairs := similarEnvironments(envs)
			if opts.JSON {
				return out.PrintJSON(pairs)
			}
			if len(pairs) == 0 {
				out.Info("This project has fewer than two environments.")
				return nil
			}
			rows := make([][]string, 0, len(pairs))
			for _, p := range pairs {
				rows = append(rows, []string{
					p.A + " ↔ " + p.B,
					fmt.Sprintf("%d%%", p.ValuePercent),
					fmt.Sprintf("%d of %d", p.SameValues, p.CommonKeys),
					fmt.Sprintf("%d%%", p.KeyPercent),
					fmt.Sprintf("%d / %d", p.OnlyA, p.OnlyB),
				})
			}
			out.Table([]string{"PAIR", "SAME VALUES", "IDENTICAL", "SAME KEYS", "ONLY IN A / B"}, rows)
			out.Info("Two environments with mostly identical values may be copies: check that they should not differ. Values are never printed.")
			return nil
		},
	}
}
