//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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
// executable. Linux is resolved via /proc/<pid>/exe (tolerating a " (deleted)"
// marker after the binary was replaced); other unixes fall back to `ps` because
// /proc is Linux-only.
func processMatchesPath(pid int, recorded string) bool {
	if pid == os.Getpid() {
		if self, err := os.Executable(); err == nil {
			return samePath(self, recorded)
		}
	}
	if resolved, err := os.Readlink(filepath.Join("/proc", strconv.Itoa(pid), "exe")); err == nil {
		return samePath(resolved, recorded)
	}
	// No /proc (darwin/BSD): compare the process command name via ps, with a
	// bounded wait so a hung ps cannot block an identity check.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return false
	}
	comm := strings.TrimSpace(string(out))
	if comm == "" {
		return false
	}
	// Accept a full-path match, or a basename match only when ps printed a bare
	// name (no directory separator), so an unrelated process at the same path
	// cannot be mistaken for ours.
	if samePath(comm, recorded) {
		return true
	}
	return !strings.ContainsRune(comm, filepath.Separator) && comm == filepath.Base(recorded)
}
