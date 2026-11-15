//go:build !linux && !darwin && !windows

package cli

import (
	"errors"
	"os"
)

const ptySupported = false

func openPTY() (master, slave *os.File, err error) {
	return nil, nil, errors.New("pseudo-terminals are not supported on this system")
}
