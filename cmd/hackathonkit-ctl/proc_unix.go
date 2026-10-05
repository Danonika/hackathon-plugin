//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// processAlive reports whether pid is an existing process. On POSIX a signal 0
// probe is supported; EPERM still means the process exists.
func processAlive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}

// terminateProcess asks the process to stop gracefully (SIGTERM on POSIX).
func terminateProcess(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}

// processMatchesPath reports whether the running process at pid is the recorded
// executable, comparing /proc/<pid>/exe (tolerating a " (deleted)" marker after
// the binary was replaced).
func processMatchesPath(pid int, recorded string) bool {
	if pid == os.Getpid() {
		if self, err := os.Executable(); err == nil {
			return samePath(self, recorded)
		}
	}
	resolved, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe"))
	if err != nil {
		return false
	}
	return samePath(resolved, recorded)
}
