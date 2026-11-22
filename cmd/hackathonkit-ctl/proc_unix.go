//go:build unix

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
	defer process.Release()
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
	// A bare command name does not establish executable identity.
	return filepath.IsAbs(comm) && samePath(comm, recorded)
}

// processBirth identifies a process instance independently of the executable
// pathname, which can be shared by every subcommand of this binary.
func processBirth(pid int) string {
	if raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat")); err == nil {
		if end := strings.LastIndexByte(string(raw), ')'); end >= 0 {
			fields := strings.Fields(string(raw[end+1:]))
			if len(fields) > 19 {
				return fields[19]
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "lstart=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func processLooksLikeDaemon(pid int, recorded string) bool {
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err == nil {
		return legacyDaemonArgs(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00"), recorded)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-ww", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	text := strings.TrimSpace(string(out))
	if recorded != "" && text == recorded+" daemon foreground" {
		return true
	}
	return legacyDaemonArgs(strings.Fields(text), recorded)
}
