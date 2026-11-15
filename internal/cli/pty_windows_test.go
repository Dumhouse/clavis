//go:build windows

package cli

import (
	"io"
	"os/exec"
	"strings"
	"testing"
)

// A program run on the Windows pseudo console writes through it, so its
// output reaches Master and the exit status comes back from Wait.
func TestStartOnPTYRunsAProgramOnConPTY(t *testing.T) {
	if err := ptyAvailable(); err != nil {
		t.Skipf("no pseudo console on this Windows: %v", err)
	}
	session, err := startOnPTY(exec.Command("cmd", "/c", "echo conpty-works && exit 3"))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	out, _ := io.ReadAll(session.Master)
	err = session.Wait()
	var exit *ExitError
	if !asExitError(err, &exit) || exit.Code != 3 {
		t.Fatalf("exit status: %v", err)
	}
	if !strings.Contains(string(out), "conpty-works") {
		t.Fatalf("output = %q", out)
	}
}

func asExitError(err error, target **ExitError) bool {
	e, ok := err.(*ExitError)
	if ok {
		*target = e
	}
	return ok
}
