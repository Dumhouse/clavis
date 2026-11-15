//go:build !linux && !darwin && !windows

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

func (s *ptySession) Wait() error {
	return errors.New("pseudo-terminals are not supported on this system")
}

func (s *ptySession) Kill() {}

func ptyAvailable() error { return errors.New("pseudo-terminals are not supported on this system") }
