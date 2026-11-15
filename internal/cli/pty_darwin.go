//go:build darwin

package cli

import (
	"bytes"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

const ptySupported = true

// openPTY returns a new pseudo-terminal pair.
func openPTY() (master, slave *os.File, err error) {
	master, err = os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(master.Fd())
	if err = unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err == nil {
		err = unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0)
	}
	var name [128]byte
	if err == nil {
		_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&name[0])))
		if errno != 0 {
			err = errno
		}
	}
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	path := string(name[:bytes.IndexByte(name[:], 0)])
	slave, err = os.OpenFile(path, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		_ = master.Close()
		return nil, nil, err
	}
	return master, slave, nil
}
