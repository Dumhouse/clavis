//go:build windows

package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptySupported is true on Windows 10 1809 and later, which have ConPTY.
const ptySupported = true

// ptySession is a child running on a Windows pseudo console (ConPTY): it sees
// a real console, so prompts, colors and line editing work.
type ptySession struct {
	Master  *os.File
	process windows.Handle
	restore func()
}

// ptyAvailable reports whether this Windows can create a pseudo console.
func ptyAvailable() error {
	inRead, inWrite, outRead, outWrite, err := conPTYPipes()
	if err != nil {
		return err
	}
	defer closeHandles(inRead, inWrite, outRead, outWrite)
	var pc windows.Handle
	if err := windows.CreatePseudoConsole(windows.Coord{X: 80, Y: 24}, inRead, outWrite, 0, &pc); err != nil {
		return fmt.Errorf("this Windows has no pseudo console (needs Windows 10 version 1809 or later): %w", err)
	}
	windows.ClosePseudoConsole(pc)
	return nil
}

func conPTYPipes() (inRead, inWrite, outRead, outWrite windows.Handle, err error) {
	if err = windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return
	}
	if err = windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		closeHandles(inRead, inWrite)
	}
	return
}

func closeHandles(handles ...windows.Handle) {
	for _, h := range handles {
		if h != 0 && h != windows.InvalidHandle {
			_ = windows.CloseHandle(h)
		}
	}
}

func consoleSize() windows.Coord {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(windows.Handle(os.Stdout.Fd()), &info); err == nil {
		return windows.Coord{
			X: info.Window.Right - info.Window.Left + 1,
			Y: info.Window.Bottom - info.Window.Top + 1,
		}
	}
	return windows.Coord{X: 80, Y: 24}
}

// startOnPTY starts child on a new pseudo console. Windows has no way to hand
// an exec.Cmd a pseudo console, so the process is created directly and child
// is used only for its path, arguments, environment and directory.
func startOnPTY(child *exec.Cmd) (*ptySession, error) {
	inRead, inWrite, outRead, outWrite, err := conPTYPipes()
	if err != nil {
		return nil, err
	}
	var pc windows.Handle
	if err := windows.CreatePseudoConsole(consoleSize(), inRead, outWrite, 0, &pc); err != nil {
		closeHandles(inRead, inWrite, outRead, outWrite)
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	// The pseudo console owns these two ends now.
	closeHandles(inRead, outWrite)

	fail := func(err error) (*ptySession, error) {
		windows.ClosePseudoConsole(pc)
		closeHandles(inWrite, outRead)
		return nil, err
	}

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fail(err)
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON handle itself, not a pointer to it.
	handleBits := *(*unsafe.Pointer)(unsafe.Pointer(&pc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, handleBits, unsafe.Sizeof(pc)); err != nil {
		return fail(err)
	}

	exe := child.Path
	if exe == "" || !strings.ContainsAny(exe, `\/`) {
		if resolved, lookErr := exec.LookPath(child.Args[0]); lookErr == nil {
			exe = resolved
		}
	}
	cmdLine, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{exe}, child.Args[1:]...)))
	if err != nil {
		return fail(err)
	}
	var dir *uint16
	if child.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(child.Dir); err != nil {
			return fail(err)
		}
	}
	env := child.Env
	if env == nil {
		env = os.Environ()
	}
	envBlock, err := environmentBlock(env)
	if err != nil {
		return fail(err)
	}

	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = attrs.List()
	var pi windows.ProcessInformation
	appName, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return fail(err)
	}
	if err := windows.CreateProcess(appName, cmdLine, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&envBlock[0], dir, &si.StartupInfo, &pi); err != nil {
		return fail(fmt.Errorf("%s: %w", child.Args[0], err))
	}
	_ = windows.CloseHandle(pi.Thread)

	// Raw console input with VT sequences, so keys reach the program as
	// typed; VT output so the program's colors and cursor moves display.
	stdin := windows.Handle(os.Stdin.Fd())
	stdout := windows.Handle(os.Stdout.Fd())
	var inMode, outMode uint32
	hadIn := windows.GetConsoleMode(stdin, &inMode) == nil
	hadOut := windows.GetConsoleMode(stdout, &outMode) == nil
	if hadIn {
		raw := (inMode &^ (windows.ENABLE_ECHO_INPUT | windows.ENABLE_LINE_INPUT | windows.ENABLE_PROCESSED_INPUT)) | windows.ENABLE_VIRTUAL_TERMINAL_INPUT
		_ = windows.SetConsoleMode(stdin, raw)
	}
	if hadOut {
		_ = windows.SetConsoleMode(stdout, outMode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}

	master := os.NewFile(uintptr(outRead), "conpty-out")
	keys := os.NewFile(uintptr(inWrite), "conpty-in")
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				if _, werr := keys.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	// The output pipe only ends when the pseudo console closes, so close it
	// as soon as the program exits; what it already wrote is still read.
	go func() {
		_, _ = windows.WaitForSingleObject(pi.Process, windows.INFINITE)
		windows.ClosePseudoConsole(pc)
	}()

	return &ptySession{
		Master:  master,
		process: pi.Process,
		restore: func() {
			if hadIn {
				_ = windows.SetConsoleMode(stdin, inMode)
			}
			if hadOut {
				_ = windows.SetConsoleMode(stdout, outMode)
			}
			_ = keys.Close()
			_ = master.Close()
			_ = windows.CloseHandle(pi.Process)
		},
	}, nil
}

// environmentBlock builds the UTF-16 block CreateProcess expects.
func environmentBlock(env []string) ([]uint16, error) {
	var block []uint16
	for _, entry := range env {
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, errors.New("environment entry contains a NUL byte")
		}
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			return nil, err
		}
		block = append(block, encoded...) // UTF16FromString adds the terminating NUL
	}
	return append(block, 0), nil
}

// Wait waits for the program to exit and returns its exit status.
func (s *ptySession) Wait() error {
	if _, err := windows.WaitForSingleObject(s.process, windows.INFINITE); err != nil {
		return err
	}
	var code uint32
	if err := windows.GetExitCodeProcess(s.process, &code); err != nil {
		return err
	}
	if code != 0 {
		return &ExitError{Code: int(code), Err: fmt.Errorf("exit status %d", code), Silent: true}
	}
	return nil
}

// Kill stops the program.
func (s *ptySession) Kill() { _ = windows.TerminateProcess(s.process, 1) }

// Close puts the real console back and closes the pipes.
func (s *ptySession) Close() { s.restore() }
