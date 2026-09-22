//go:build !windows

package main

import (
	"os"
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
	return err == nil || err == syscall.EPERM
}

// terminateProcess asks the process to stop gracefully (SIGTERM on POSIX).
func terminateProcess(process *os.Process) error {
	return process.Signal(syscall.SIGTERM)
}
