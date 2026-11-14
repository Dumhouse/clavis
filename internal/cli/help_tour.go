package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// tourStep is one stop of `clavis help --tour`: what it is for, the command
// to run, and a check that it really worked.
type tourStep struct {
	Title   string
	Why     string
	Command string
	// Check reports whether the step's result is in place; nil trusts the
	// command's exit status.
	Check func() (ok bool, fix string)
}

// tourSteps is the first-run path: sign in, link, store, run, verify.
func tourSteps(opts *Options) []tourStep {
	ctx := func() *resolvedContext {
		r, err := opts.resolveContext(false)
		if err != nil {
			return &resolvedContext{}
		}
		return r
	}
	return []tourStep{
		{
			Title:   "Sign in",
			Why:     "Opens your browser to approve this computer. The token goes to your OS keychain.",
			Command: "clavis login",
			Check: func() (bool, string) {
				r := ctx()
				return r.Token != "", "run `clavis login --server " + r.Server + "`"
			},
		},
		{
			Title:   "Link this folder to a project",
			Why:     "Writes .clavis.yaml (no secrets in it), so commands here know the project and environment.",
			Command: "clavis init",
			Check: func() (bool, string) {
				return ctx().ConfigPath != "", "run `clavis init` in the folder of your app"
			},
		},
		{
			Title:   "Store a value",
			Why:     "A plain variable, so nothing sensitive is typed during the tour. You can delete it afterwards.",
			Command: "clavis secrets set CLAVIS_TOUR_HELLO hello --kind variable",
		},
		{
			Title:   "Run something with it",
			Why:     "`clavis run` puts the values into one command's environment, and masks them in its output.",
			Command: `clavis run -- sh -c 'echo "value: $CLAVIS_TOUR_HELLO"'`,
		},
		{
			Title:   "Check the setup",
			Why:     "`clavis doctor` is the first thing to run when something doesn't work.",
			Command: "clavis doctor",
		},
	}
}

// runTour walks the steps, asking before it runs anything. run executes a
// command line; in and out are the terminal.
func runTour(in io.Reader, out *Output, steps []tourStep, run func(string) error) error {
	reader := bufio.NewReader(in)
	p := func(format string, args ...any) { _, _ = fmt.Fprintf(out.Out, format, args...) }
	ask := func(prompt string) string {
		p("%s", prompt)
		line, _ := reader.ReadString('\n')
		return strings.ToLower(strings.TrimSpace(line))
	}

	p("%s\n%s\n", out.style("Clavis tour", helpAccent), out.style("Five steps from nothing to running your app with secrets. Nothing runs until you say so.", helpDim))
	done := 0
	for i, step := range steps {
		p("\n%s %s\n", out.style(fmt.Sprintf("%d/%d", i+1, len(steps)), helpFaint), out.style(step.Title, helpTitle))
		p("  %s\n  %s %s\n", step.Why, out.style("$", helpFaint), out.style(step.Command, helpCode))
		for {
			answer := ask(out.style("  Enter to run · s to skip · q to quit › ", helpFaint))
			if answer == "q" {
				p("\nStopped after %d of %d steps. Come back with `clavis help --tour`.\n", done, len(steps))
				return nil
			}
			if answer == "s" {
				break
			}
			if answer != "" {
				continue
			}
			err := run(step.Command)
			ok, fix := err == nil, "see the message above"
			if err == nil && step.Check != nil {
				ok, fix = step.Check()
			}
			if ok {
				p("%s\n", out.style("  ✓ done", "34d399"))
				done++
				break
			}
			p("%s %s\n", out.style("  ✗ not yet:", "f87171"), fix)
			if retry := ask(out.style("  Enter to try again · s to skip · q to quit › ", helpFaint)); retry == "q" {
				p("\nStopped after %d of %d steps.\n", done, len(steps))
				return nil
			} else if retry == "s" {
				break
			}
		}
	}
	p("\n%s %d of %d steps done. `clavis help` has everything else.\n", out.style("✓", "34d399"), done, len(steps))
	return nil
}
