package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/dumhouse/clavis/internal/updatecheck"
	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/problem"
)

// doctorStatus is the outcome of one check.
type doctorStatus string

const (
	doctorOK   doctorStatus = "ok"
	doctorWarn doctorStatus = "warn"
	doctorFail doctorStatus = "fail"
	doctorSkip doctorStatus = "skip"
)

// doctorCheck is one line of the report. Fix is a command or step that
// resolves a warning or failure.
type doctorCheck struct {
	Section string       `json:"section"`
	Name    string       `json:"name"`
	Status  doctorStatus `json:"status"`
	Detail  string       `json:"detail"`
	Fix     string       `json:"fix,omitempty"`
}

// clockSkewLimit is how far this computer's clock may drift from the
// server's before sign-in codes and short-lived tokens start failing.
const clockSkewLimit = time.Minute

func newDoctorCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this computer's Clavis setup and say how to fix problems",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			checks := runDoctor(ctx, opts)
			out := opts.output()
			failed := 0
			for _, check := range checks {
				if check.Status == doctorFail {
					failed++
				}
			}
			if opts.JSON {
				if err := out.PrintJSON(map[string]any{"checks": checks, "failed": failed}); err != nil {
					return err
				}
			} else {
				printDoctor(out, checks)
			}
			if failed > 0 {
				return &ExitError{Code: ExitGeneral, Err: fmt.Errorf("%d check(s) failed", failed), Silent: true}
			}
			return nil
		},
	}
}

// runDoctor runs every check in order; later checks are skipped when the
// ones they depend on failed, so the report points at the first real cause.
func runDoctor(ctx context.Context, opts *Options) []doctorCheck {
	var checks []doctorCheck
	add := func(c doctorCheck) { checks = append(checks, c) }

	// This CLI.
	install := currentInstall("")
	add(doctorCheck{Section: "CLI", Name: "Version", Status: doctorOK,
		Detail: fmt.Sprintf("%s (%s)", version.Version, install.Label)})
	latestCtx, cancelLatest := context.WithTimeout(ctx, 3*time.Second)
	latest, latestErr := fetchLatestRelease(latestCtx)
	cancelLatest()
	switch {
	case latestErr != nil:
		add(doctorCheck{Section: "CLI", Name: "Latest release", Status: doctorSkip,
			Detail: "could not reach GitHub to compare"})
	case updatecheck.Newer(latest, version.Version):
		fix := "clavis upgrade"
		if !selfUpgrade[install.Method] && install.Command != "" {
			fix = install.Command
		}
		add(doctorCheck{Section: "CLI", Name: "Latest release", Status: doctorWarn,
			Detail: latest + " is available", Fix: fix})
	default:
		add(doctorCheck{Section: "CLI", Name: "Latest release", Status: doctorOK, Detail: "up to date (" + latest + ")"})
	}

	// Project configuration.
	resolved, err := opts.resolveContext(false)
	if err != nil {
		add(doctorCheck{Section: "Project", Name: ".clavis.yaml", Status: doctorFail,
			Detail: err.Error(), Fix: "Fix or delete .clavis.yaml, then run: clavis init"})
		return checks
	}
	if resolved.ConfigPath != "" {
		add(doctorCheck{Section: "Project", Name: ".clavis.yaml", Status: doctorOK, Detail: resolved.ConfigPath})
	} else {
		add(doctorCheck{Section: "Project", Name: ".clavis.yaml", Status: doctorWarn,
			Detail: "none in this folder or its parents", Fix: "clavis init"})
	}
	if names := clavisEnvNames(); len(names) > 0 {
		add(doctorCheck{Section: "Project", Name: "Environment variables", Status: doctorOK,
			Detail: strings.Join(names, ", ") + " (these override .clavis.yaml)"})
	}
	var committedEnvFiles []string
	if cfg, _, err := loadProjectConfig("."); err == nil {
		committedEnvFiles = cfg.CommittedEnvFiles
	}
	for _, check := range envFileDoctorChecks(ctx, resolved.ConfigPath, committedEnvFiles) {
		add(check)
	}

	// Server.
	server := resolved.Server
	serverDetail := fmt.Sprintf("%s (from %s)", server, resolved.ServerFrom)
	if resolved.ServerFrom == "the default" {
		add(doctorCheck{Section: "Server", Name: "Address", Status: doctorWarn, Detail: serverDetail,
			Fix: "clavis login --server https://your-clavis-server"})
	} else {
		add(doctorCheck{Section: "Server", Name: "Address", Status: doctorOK, Detail: serverDetail})
	}
	if u, err := url.Parse(server); err == nil && u.Scheme == "http" && !isLocalHost(u.Hostname()) {
		add(doctorCheck{Section: "Server", Name: "HTTPS", Status: doctorWarn,
			Detail: "plain HTTP: your token and secrets cross the network unencrypted",
			Fix:    "Serve Clavis over HTTPS (CLAVIS_TLS=self-signed or acme) and log in with https://"})
	}

	reach := probeServer(ctx, server)
	switch {
	case reach.untrusted != nil:
		add(doctorCheck{Section: "Server", Name: "Certificate", Status: doctorFail,
			Detail: "the server's certificate is not trusted on this computer",
			Fix:    "clavis trust add " + server})
		return checks
	case reach.err != nil:
		add(doctorCheck{Section: "Server", Name: "Reachable", Status: doctorFail,
			Detail: reach.err.Error(),
			Fix:    "Check the address and that the server runs; on the server: docker logs clavis"})
		return checks
	default:
		add(doctorCheck{Section: "Server", Name: "Reachable", Status: doctorOK,
			Detail: fmt.Sprintf("answered in %s", reach.latency.Round(time.Millisecond))})
	}
	if reach.version != "" {
		status, detail, fix := doctorOK, reach.version, ""
		if updatecheck.Drifted(version.Version, reach.version) {
			status = doctorWarn
			detail = fmt.Sprintf("%s, but this CLI is %s; commands may not match", reach.version, version.Version)
			if updatecheck.Newer(reach.version, version.Version) {
				fix = "clavis upgrade"
			} else {
				fix = "Upgrade the server (clavis update shows how)"
			}
		}
		add(doctorCheck{Section: "Server", Name: "Server version", Status: status, Detail: detail, Fix: fix})
	}
	if !reach.serverTime.IsZero() {
		skew := time.Since(reach.serverTime) - reach.latency/2
		if skew < 0 {
			skew = -skew
		}
		if skew > clockSkewLimit {
			add(doctorCheck{Section: "Server", Name: "Clock", Status: doctorWarn,
				Detail: fmt.Sprintf("this computer is %s off from the server", skew.Round(time.Second)),
				Fix:    "Turn on automatic time sync (two-factor codes and short-lived tokens depend on it)"})
		} else {
			add(doctorCheck{Section: "Server", Name: "Clock", Status: doctorOK, Detail: "in sync with the server"})
		}
	}

	// Sign-in.
	tokenFrom := credentialStore().Description()
	if os.Getenv("CLAVIS_TOKEN") != "" {
		tokenFrom = "CLAVIS_TOKEN"
	}
	if resolved.Token == "" {
		add(doctorCheck{Section: "Sign-in", Name: "Credential", Status: doctorFail,
			Detail: "not signed in to " + server, Fix: "clavis login --server " + server})
		return checks
	}
	c, err := resolved.newClient()
	if err != nil {
		add(doctorCheck{Section: "Sign-in", Name: "Credential", Status: doctorFail, Detail: err.Error()})
		return checks
	}
	var me whoAmIResponse
	if err := c.Get(ctx, "/api/v1/auth/me", &me); err != nil {
		check := doctorCheck{Section: "Sign-in", Name: "Credential", Status: doctorFail,
			Detail: "the saved token was refused (" + tokenFrom + ")", Fix: "clavis login --server " + server}
		var p *problem.Problem
		if !errors.As(err, &p) || (p.Status != http.StatusUnauthorized && p.Status != http.StatusForbidden) {
			check.Detail = err.Error()
			check.Fix = ""
		}
		add(check)
		return checks
	}
	who := me.User.Email
	if who == "" {
		who = me.User.Name
	}
	if who == "" {
		who = "a machine or agent token"
	}
	add(doctorCheck{Section: "Sign-in", Name: "Credential", Status: doctorOK,
		Detail: fmt.Sprintf("%s (token from %s)", who, tokenFrom)})

	// Project access.
	if resolved.Org == "" || resolved.Project == "" {
		add(doctorCheck{Section: "Access", Name: "Project", Status: doctorSkip,
			Detail: "no organization or project chosen", Fix: "clavis init"})
		return checks
	}
	var detail projectDetail
	if err := c.Get(ctx, projectBase(resolved), &detail); err != nil {
		add(doctorCheck{Section: "Access", Name: "Project", Status: doctorFail,
			Detail: fmt.Sprintf("%s/%s: %v", resolved.Org, resolved.Project, err),
			Fix:    "Check the slugs with clavis whoami, or ask an admin for access"})
		return checks
	}
	add(doctorCheck{Section: "Access", Name: "Project", Status: doctorOK,
		Detail: fmt.Sprintf("%s/%s", resolved.Org, detail.Slug)})
	slugs := make([]string, 0, len(detail.Environments))
	found := false
	for _, env := range detail.Environments {
		slugs = append(slugs, env.Slug)
		if env.Slug == resolved.Env {
			found = true
		}
	}
	if found {
		add(doctorCheck{Section: "Access", Name: "Environment", Status: doctorOK, Detail: resolved.Env})
	} else {
		add(doctorCheck{Section: "Access", Name: "Environment", Status: doctorFail,
			Detail: fmt.Sprintf("%q is not in this project (it has %s)", resolved.Env, strings.Join(slugs, ", ")),
			Fix:    "clavis init --env " + firstNonEmpty(firstSlug(slugs), "dev")})
	}
	return checks
}

func firstSlug(slugs []string) string {
	if len(slugs) == 0 {
		return ""
	}
	return slugs[0]
}

// clavisEnvNames lists the CLAVIS_* variables that change the CLI's context.
// Only names are shown, never values.
func clavisEnvNames() []string {
	var names []string
	for _, name := range []string{"CLAVIS_SERVER", "CLAVIS_ORG", "CLAVIS_PROJECT", "CLAVIS_ENV", "CLAVIS_TOKEN", "CLAVIS_CA_CERT", "CLAVIS_SERVER_FINGERPRINT"} {
		if os.Getenv(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func isLocalHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

type serverProbe struct {
	latency    time.Duration
	version    string
	serverTime time.Time
	untrusted  *untrustedCertError
	err        error
}

// probeServer checks that the server answers, and reads its version and
// clock without signing in.
func probeServer(ctx context.Context, server string) serverProbe {
	var result serverProbe
	if untrusted := probeServerTLS(ctx, server); untrusted != nil {
		result.untrusted = untrusted
		return result
	}
	hc, err := httpClientFor(server, 10*time.Second)
	if err != nil {
		result.err = err
		return result
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(server, "/")+"/api/v1/version", nil)
	if err != nil {
		result.err = err
		return result
	}
	request.Header.Set("User-Agent", "clavis-cli/"+version.Version)
	started := time.Now()
	response, err := hc.Do(request) //nolint:gosec // G704: the user's own configured server
	if err != nil {
		var untrusted *untrustedCertError
		if errors.As(err, &untrusted) {
			result.untrusted = untrusted
			return result
		}
		// Keep the cause ("connection refused", "no such host"), not the URL.
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			result.err = fmt.Errorf("no answer: %w", opErr.Err)
		} else {
			result.err = fmt.Errorf("no answer: %w", err)
		}
		return result
	}
	defer func() { _ = response.Body.Close() }()
	result.latency = time.Since(started)
	if date, err := http.ParseTime(response.Header.Get("Date")); err == nil {
		result.serverTime = date
	}
	if response.StatusCode != http.StatusOK {
		result.err = fmt.Errorf("answered %s; is this a Clavis server?", response.Status)
		return result
	}
	var body struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<16)).Decode(&body); err == nil {
		result.version = body.Version
	}
	return result
}

// printDoctor renders the report grouped by section, with fixes under the
// checks that need one.
func printDoctor(out *Output, checks []doctorCheck) {
	section := ""
	warned, failed := 0, 0
	for _, check := range checks {
		if check.Section != section {
			if section != "" {
				_, _ = fmt.Fprintln(out.Out)
			}
			section = check.Section
			_, _ = fmt.Fprintln(out.Out, out.style(strings.ToUpper(section), "9ca3af"))
		}
		var mark string
		switch check.Status {
		case doctorOK:
			mark = out.style("✓", "34d399")
		case doctorWarn:
			mark = out.style("!", "f59e0b")
			warned++
		case doctorFail:
			mark = out.style("✗", "f87171")
			failed++
		default:
			mark = out.style("–", "6b7280")
		}
		_, _ = fmt.Fprintf(out.Out, "  %s %-22s %s\n", mark, check.Name, check.Detail)
		if check.Fix != "" && check.Status != doctorOK {
			_, _ = fmt.Fprintf(out.Out, "    %s %s\n", out.style("→", "6b7280"), out.style(check.Fix, "93c5fd"))
		}
	}
	_, _ = fmt.Fprintln(out.Out)
	switch {
	case failed > 0:
		_, _ = fmt.Fprintln(out.Out, out.style(fmt.Sprintf("%d problem(s) to fix, %d warning(s). Start with the first ✗.", failed, warned), "f87171"))
	case warned > 0:
		_, _ = fmt.Fprintln(out.Out, out.style(fmt.Sprintf("Everything works; %d warning(s) worth a look.", warned), "f59e0b"))
	default:
		_, _ = fmt.Fprintln(out.Out, out.style("Everything looks good.", "34d399"))
	}
}
