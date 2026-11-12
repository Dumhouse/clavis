package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/dumhouse/clavis/internal/version"
	"github.com/dumhouse/clavis/pkg/problem"
)

func TestVersionJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	opts := &Options{JSON: true, Output: NewTestOutput(&out, &errOut, true, false)}
	cmd := NewRootCommandWithOptions(opts)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v (%s)", err, out.String())
	}
	if got["version"] != version.Version || got["commit"] == "" {
		t.Fatalf("version = %v", got)
	}
}

func TestVersionPlain(t *testing.T) {
	var out, errOut bytes.Buffer
	opts := &Options{Output: NewTestOutput(&out, &errOut, false, false)}
	cmd := NewRootCommandWithOptions(opts)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "clavis "+version.Version) {
		t.Fatalf("output = %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("plain output contains ANSI escapes")
	}
}

func TestRootHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	cmd := NewRootCommandWithOptions(&Options{Output: NewTestOutput(&out, &errOut, false, false)})
	cmd.SetArgs(nil)
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "server") || !strings.Contains(out.String(), "version") {
		t.Fatalf("help = %q", out.String())
	}
}

func TestExitCodes(t *testing.T) {
	if ExitCode(nil) != ExitOK {
		t.Fatal("nil should be OK")
	}
	if ExitCode(&ExitError{Code: ExitAuth, Err: errors.New("x")}) != ExitAuth {
		t.Fatal("explicit exit code lost")
	}
	cases := map[int]int{
		http.StatusUnauthorized:        ExitAuth,
		http.StatusForbidden:           ExitPermission,
		http.StatusNotFound:            ExitNotFound,
		http.StatusBadRequest:          ExitValidation,
		http.StatusUnprocessableEntity: ExitValidation,
		http.StatusInternalServerError: ExitGeneral,
	}
	for status, want := range cases {
		if got := ExitCode(problem.New(status, "x", "x", "")); got != want {
			t.Errorf("status %d -> %d, want %d", status, got, want)
		}
	}
	if ExitCode(errors.New("boom")) != ExitGeneral {
		t.Fatal("generic error")
	}
}

func TestOutputRendering(t *testing.T) {
	var out, errOut bytes.Buffer
	o := NewTestOutput(&out, &errOut, false, false)
	o.Success("saved %s", "KEY")
	o.Info("hello")
	o.Warn("careful")
	o.Errorf("broken")

	if !strings.Contains(out.String(), "saved KEY") || !strings.Contains(out.String(), "hello") {
		t.Fatalf("stdout = %q", out.String())
	}
	if !strings.Contains(errOut.String(), "careful") || !strings.Contains(errOut.String(), "broken") {
		t.Fatalf("stderr = %q", errOut.String())
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatal("color disabled output contains escapes")
	}
}

func TestOutputColor(t *testing.T) {
	var out, errOut bytes.Buffer
	o := NewTestOutput(&out, &errOut, false, true)
	if got := o.EnvLabel("production"); !strings.Contains(got, "\x1b[") {
		t.Fatalf("colored label = %q", got)
	}
	if got := o.EnvLabel("unknown"); got != "unknown" {
		t.Fatalf("unknown label = %q", got)
	}
	o.Table([]string{"NAME"}, [][]string{{"value"}})
	if !strings.Contains(out.String(), "value") {
		t.Fatalf("table = %q", out.String())
	}
}

func TestOutputEnvLabelPlain(t *testing.T) {
	var out, errOut bytes.Buffer
	o := NewTestOutput(&out, &errOut, false, false)
	if o.EnvLabel("production") != "production" {
		t.Fatal("plain label changed")
	}
}

func TestTablePlain(t *testing.T) {
	var out, errOut bytes.Buffer
	o := NewTestOutput(&out, &errOut, false, false)
	o.Table([]string{"KEY", "ENV"}, [][]string{{"A", "dev"}, {"LONG", "prod"}})
	text := out.String()
	if !strings.Contains(text, "KEY") || !strings.Contains(text, "LONG") {
		t.Fatalf("table = %q", text)
	}
}

func TestPrintJSON(t *testing.T) {
	var out, errOut bytes.Buffer
	o := NewTestOutput(&out, &errOut, true, false)
	if err := o.PrintJSON(map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\"a\": 1") {
		t.Fatalf("json = %q", out.String())
	}
}

func TestColorEnabled(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(f) {
		t.Fatal("NO_COLOR ignored")
	}
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled(f) {
		t.Fatal("TERM=dumb ignored")
	}
}

func TestExitError(t *testing.T) {
	inner := errors.New("inner")
	err := &ExitError{Code: ExitLeak, Err: inner}
	if err.Error() != "inner" || !errors.Is(err, inner) {
		t.Fatal("exit error wrapping broken")
	}
}
