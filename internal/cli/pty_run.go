//go:build linux || darwin

package cli

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// ptySession is a child running on its own pseudo-terminal that mirrors the
// real one: same size, keystrokes passed through, programs see a terminal.
type ptySession struct {
	Master  *os.File
	restore func()
}

// startOnPTY starts child on a new pseudo-terminal. Programs that draw
// prompts, colors or line editing only do so on a terminal, which is why
// `rails c` showed no prompt when its output was a pipe. Output comes from
// Master; Close puts the real terminal back.
func startOnPTY(child *exec.Cmd) (*ptySession, error) {
	master, slave, err := openPTY()
	if err != nil {
		return nil, err
	}
	defer func() { _ = slave.Close() }()

	stdin := int(os.Stdin.Fd())
	if size, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil {
		_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, size)
	}
	child.Stdin, child.Stdout, child.Stderr = slave, slave, slave
	child.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := child.Start(); err != nil {
		_ = master.Close()
		return nil, err
	}

	// Raw mode: keys go to the program as typed (Ctrl-C included, which the
	// pseudo-terminal turns into SIGINT for the program, not for us).
	saved, rawErr := term.MakeRaw(stdin)
	resize := make(chan os.Signal, 1)
	signal.Notify(resize, syscall.SIGWINCH)
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-resize:
				if size, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil {
					_ = unix.IoctlSetWinsize(int(master.Fd()), unix.TIOCSWINSZ, size)
				}
			case <-stop:
				return
			}
		}
	}()
	// Keystrokes to the program. This read can't be cancelled, so the
	// goroutine ends with the process or at the next keypress.
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if _, werr := master.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return &ptySession{
		Master: master,
		restore: func() {
			signal.Stop(resize)
			close(stop)
			if rawErr == nil {
				_ = term.Restore(stdin, saved)
			}
			_ = master.Close()
		},
	}, nil
}

// Close restores the real terminal and closes the pseudo-terminal.
func (s *ptySession) Close() { s.restore() }
