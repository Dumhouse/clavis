//go:build !linux && !darwin

package cli

import (
	"errors"
	"os"
	"os/exec"
)

type ptySession struct{ Master *os.File }

func startOnPTY(*exec.Cmd) (*ptySession, error) {
	return nil, errors.New("pseudo-terminals are not supported on this system")
}

func (s *ptySession) Close() {}
