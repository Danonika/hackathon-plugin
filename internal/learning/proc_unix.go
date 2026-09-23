//go:build !windows

package learning

import (
	"errors"
	"os"
	"syscall"
)

// processAlive reports whether pid is a live process. On POSIX a signal 0 probe
// is supported; EPERM still means the process exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
