package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"golang.org/x/term"

	"github.com/dumhouse/clavis/pkg/problem"
)

// Stable exit codes from docs/cli.md.
const (
	ExitOK         = 0
	ExitGeneral    = 1
	ExitUsage      = 2
	ExitAuth       = 3
	ExitPermission = 4
	ExitNotFound   = 5
	ExitValidation = 6
	ExitLeak       = 7
)

// ExitError carries an exit code through cobra.
type ExitError struct {
	Code   int
	Err    error
	Silent bool // do not print the error (for child process exit codes)
}

// Error implements error.
func (e *ExitError) Error() string { return e.Err.Error() }

// Unwrap supports errors.Is/As.
func (e *ExitError) Unwrap() error { return e.Err }

// ExitCode maps an error to a stable process exit code.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		switch p.Status {
		case http.StatusUnauthorized:
			return ExitAuth
		case http.StatusForbidden:
			return ExitPermission
		case http.StatusNotFound:
			return ExitNotFound
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return ExitValidation
		}
	}
	return ExitGeneral
}

// Output renders CLI output in human or JSON form.
type Output struct {
	JSON     bool
	Out      io.Writer
	Err      io.Writer
	Color    bool
	renderer *lipgloss.Renderer
}

// NewOutput returns output bound to stdout/stderr with color auto-detection.
func NewOutput(jsonMode bool) *Output {
	return newOutput(os.Stdout, os.Stderr, jsonMode, colorEnabled(os.Stdout) && !jsonMode)
}

// NewTestOutput is a helper for tests.
func NewTestOutput(out, errOut io.Writer, jsonMode, color bool) *Output {
	return newOutput(out, errOut, jsonMode, color)
}

func newOutput(out, errOut io.Writer, jsonMode, color bool) *Output {
	o := &Output{JSON: jsonMode, Out: out, Err: errOut, Color: color}
	o.renderer = lipgloss.NewRenderer(out)
	if color {
		// The renderer cannot sniff a TTY when tests write to buffers, so
		// honor the explicit decision.
		o.renderer.SetColorProfile(termenv.TrueColor)
	} else {
		o.renderer.SetColorProfile(termenv.Ascii)
	}
	return o
}

// PrintJSON encodes v with indentation.
func (o *Output) PrintJSON(v any) error {
	enc := json.NewEncoder(o.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Success writes a green check line.
func (o *Output) Success(format string, args ...any) {
	_, _ = fmt.Fprintf(o.Out, "%s %s\n", o.style("✓", "10b981"), fmt.Sprintf(format, args...))
}

// Info writes a muted line.
func (o *Output) Info(format string, args ...any) {
	_, _ = fmt.Fprintf(o.Out, "%s\n", fmt.Sprintf(format, args...))
}

// Errorf writes a red line to stderr.
func (o *Output) Errorf(format string, args ...any) {
	_, _ = fmt.Fprintf(o.Err, "%s %s\n", o.style("✗", "f87171"), fmt.Sprintf(format, args...))
}

// Warn writes an amber line.
func (o *Output) Warn(format string, args ...any) {
	_, _ = fmt.Fprintf(o.Err, "%s %s\n", o.style("!", "f59e0b"), fmt.Sprintf(format, args...))
}

func (o *Output) style(s, color string) string {
	if !o.Color {
		return s
	}
	return o.renderer.NewStyle().Foreground(lipgloss.Color("#" + color)).Render(s)
}

// ENVColors maps environment types to their fixed colors (docs/design.md).
var ENVColors = map[string]string{
	"development": "3b82f6",
	"preview":     "8b5cf6",
	"staging":     "14b8a6",
	"production":  "f59e0b",
}

// EnvLabel renders an environment type in its canonical color.
func (o *Output) EnvLabel(envType string) string {
	if !o.Color {
		return envType
	}
	color, ok := ENVColors[envType]
	if !ok {
		return envType
	}
	return o.renderer.NewStyle().Foreground(lipgloss.Color("#" + color)).Bold(true).Render(envType)
}

// Table renders an aligned table. With Color disabled the output is plain and
// script-friendly.
func (o *Output) Table(headers []string, rows [][]string) {
	tw := tabwriter.NewWriter(o.Out, 0, 4, 2, ' ', 0)
	for i, h := range headers {
		if i > 0 {
			_, _ = fmt.Fprint(tw, "\t")
		}
		if o.Color {
			_, _ = fmt.Fprint(tw, o.renderer.NewStyle().Bold(true).Render(h))
		} else {
			_, _ = fmt.Fprint(tw, h)
		}
	}
	_, _ = fmt.Fprintln(tw)
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				_, _ = fmt.Fprint(tw, "\t")
			}
			_, _ = fmt.Fprint(tw, cell)
		}
		_, _ = fmt.Fprintln(tw)
	}
	_ = tw.Flush()
}

func colorEnabled(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
