//go:build linux || darwin

package cli

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

// A program run on the pseudo-terminal sees a terminal, which is what makes
// `rails c`, irb and psql draw their prompts.
func TestStartOnPTYGivesTheChildATerminal(t *testing.T) {
	child := exec.Command("sh", "-c", `if [ -t 0 ] && [ -t 1 ]; then echo on-a-terminal; else echo no-terminal; fi`)
	session, err := startOnPTY(child)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, _ := io.ReadAll(session.Master) // ends with EIO when the child exits
	if err := child.Wait(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "on-a-terminal") {
		t.Fatalf("child output %q", out)
	}
}
