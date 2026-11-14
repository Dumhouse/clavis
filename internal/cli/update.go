package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/dumhouse/clavis/internal/updatecheck"
	"github.com/dumhouse/clavis/internal/version"
)

// updateState is what the CLI remembers between runs about new releases, in
// the user's cache folder. It holds no secrets.
type updateState struct {
	CheckedAt     time.Time `json:"checked_at"`
	Latest        string    `json:"latest,omitempty"`
	NotifiedAt    time.Time `json:"notified_at"`
	Server        string    `json:"server,omitempty"`
	ServerVersion string    `json:"server_version,omitempty"`
}

// How often the notice repeats while an update is waiting, and how long to
// wait before retrying a check that failed (offline, rate limited).
const (
	updateNoticeEvery = 3 * 24 * time.Hour
	updateRetryAfter  = time.Hour
)

// Seams for tests.
var (
	updateStatePath = func() (string, error) {
		dir, err := os.UserCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "clavis", "update.json"), nil
	}
	fetchLatestRelease = func(ctx context.Context) (string, error) {
		return updatecheck.Latest(ctx, nil, updatecheck.ReleasesURL)
	}
	currentInstall = func(latest string) updatecheck.Install {
		env := updatecheck.CurrentEnv()
		env.Version = version.Version
		env.Latest = latest
		return updatecheck.Detect(env)
	}
)

func loadUpdateState() updateState {
	var s updateState
	path, err := updateStatePath()
	if err != nil {
		return s
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own cache file
	if err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveUpdateState(s updateState) {
	path, err := updateStatePath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	data, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(path, data, 0o600)
}

// updateChecksDisabled is true for CI, scripts and anyone who opted out.
func updateChecksDisabled() bool {
	for _, name := range []string{"CLAVIS_NO_UPDATE_CHECK", "CI"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" && v != "0" && v != "false" {
			return true
		}
	}
	return false
}

// updateNotice returns the lines to print, if any, and whether to record that
// a notice was shown. It is pure so the policy can be tested.
func updateNotice(s updateState, now time.Time, current string, install updatecheck.Install) []string {
	if !s.NotifiedAt.IsZero() && now.Sub(s.NotifiedAt) < updateNoticeEvery {
		return nil
	}
	var lines []string
	if updatecheck.Newer(s.Latest, current) {
		line := "Clavis " + s.Latest + " is available (you have " + current + ")."
		if install.Command != "" {
			line += " Update: " + install.Command
		} else if install.Note != "" {
			line += " " + install.Note
		}
		lines = append(lines, line)
	}
	if s.ServerVersion != "" && updatecheck.Drifted(current, s.ServerVersion) {
		if updatecheck.Newer(s.ServerVersion, current) {
			lines = append(lines, "The server "+s.Server+" runs Clavis "+s.ServerVersion+
				", newer than this CLI ("+current+"). Update the CLI if commands fail.")
		} else {
			lines = append(lines, "This CLI ("+current+") is newer than the server "+s.Server+
				" ("+s.ServerVersion+"). Ask its admin to update if commands fail.")
		}
	}
	if len(lines) > 0 {
		lines = append(lines, "Run `clavis update` for details. Turn these notes off with CLAVIS_NO_UPDATE_CHECK=1.")
	}
	return lines
}

// refreshUpdateState asks for the latest release (and the server's version)
// when the last check is a day old. Failures are remembered so an offline
// machine isn't slowed down on every command.
func refreshUpdateState(ctx context.Context, opts *Options, s updateState, now time.Time, force bool) updateState {
	if !force {
		age := now.Sub(s.CheckedAt)
		if age < updatecheck.Interval && (s.Latest != "" || age < updateRetryAfter) {
			return s
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	s.CheckedAt = now
	if latest, err := fetchLatestRelease(ctx); err == nil {
		s.Latest = latest
	}
	if server, serverVersion := serverVersionFor(ctx, opts); serverVersion != "" {
		s.Server, s.ServerVersion = server, serverVersion
	}
	return s
}

// serverVersionFor reads the version of the server this folder uses, without
// signing in (the endpoint is public).
func serverVersionFor(ctx context.Context, opts *Options) (string, string) {
	resolved, err := opts.resolveContext(false)
	if err != nil || resolved.Server == defaultServer && lastServer() == "" {
		return "", ""
	}
	c, err := resolved.newAnonymousClient()
	if err != nil {
		return "", ""
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := c.Get(ctx, "/api/v1/version", &v); err != nil {
		return "", ""
	}
	return resolved.Server, v.Version
}

// quietCommands never print the update note: their output is consumed by
// programs, or they run for a long time.
var quietCommands = map[string]bool{
	"run": true, "shell": true, "mcp": true, "server": true, "export": true,
	"update": true, "completion": true, "__complete": true, "setup-token": true,
}

// maybeNotifyUpdate prints a short note after a command when a new release or
// a CLI/server version gap exists. It runs at most one network check a day and
// only in an interactive terminal.
func maybeNotifyUpdate(ctx context.Context, opts *Options, command string) {
	if opts.JSON || quietCommands[command] || updateChecksDisabled() ||
		!term.IsTerminal(int(os.Stderr.Fd())) { //nolint:gosec // G115: file descriptors fit in int
		return
	}
	now := time.Now()
	s := refreshUpdateState(ctx, opts, loadUpdateState(), now, false)
	lines := updateNotice(s, now, version.Version, currentInstall(s.Latest))
	if len(lines) > 0 {
		s.NotifiedAt = now
		out := opts.output()
		for i, line := range lines {
			if i == len(lines)-1 {
				out.Hint("%s", line)
			} else {
				out.Warn("%s", line)
			}
		}
	}
	saveUpdateState(s)
}

func newUpdateCommand(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for a new release and show how to update",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := opts.output()
			s := refreshUpdateState(cmd.Context(), opts, loadUpdateState(), time.Now(), true)
			saveUpdateState(s)
			install := currentInstall(s.Latest)
			available := updatecheck.Newer(s.Latest, version.Version)
			if opts.JSON {
				return out.PrintJSON(map[string]any{
					"current":          version.Version,
					"latest":           s.Latest,
					"update_available": available,
					"install":          install,
					"server":           s.Server,
					"server_version":   s.ServerVersion,
				})
			}
			out.Info("This CLI:       %s (%s)", version.Version, install.Label)
			switch {
			case s.Latest == "":
				out.Warn("Could not reach GitHub to check for releases.")
			case available:
				out.Info("Latest release: %s, update available", s.Latest)
			default:
				out.Success("Latest release: %s, you are up to date", s.Latest)
			}
			if s.ServerVersion != "" {
				out.Info("Server:         %s runs %s", s.Server, s.ServerVersion)
			}
			if available || install.Method == "source" {
				if install.Command != "" {
					out.Info("")
					out.Info("Update with:")
					out.Info("  %s", install.Command)
				}
				if install.Note != "" {
					out.Info("%s", install.Note)
				}
			}
			for _, line := range updateNotice(updateState{Latest: "", Server: s.Server, ServerVersion: s.ServerVersion},
				time.Now(), version.Version, install) {
				if !strings.HasPrefix(line, "Run `clavis update`") {
					out.Warn("%s", line)
				}
			}
			return nil
		},
	}
}
