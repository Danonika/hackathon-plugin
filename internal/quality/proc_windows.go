//go:build windows

package quality

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// configureProcessGroup starts each gate command in a new process group so the
// whole tree can be terminated with taskkill /T.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func ownGroup(cmd *exec.Cmd) (kill func(), release func()) {
	// Retain the precise Windows process object until cleanup is finished. A
	// PID cannot be reused while its process object has an open handle.
	var pinned windows.Handle
	owner := windows.CurrentProcess()
	err := cmd.Process.WithHandle(func(raw uintptr) {
		_ = windows.DuplicateHandle(owner, windows.Handle(raw), owner, &pinned, 0, false, windows.DUPLICATE_SAME_ACCESS)
	})
	if err != nil || pinned == 0 {
		return func() { _ = cmd.Process.Kill() }, func() {}
	}
	return func() {
		state, err := windows.WaitForSingleObject(pinned, 0)
		if err == nil && state != windows.WAIT_OBJECT_0 {
			killWindowsTree(cmd)
		} else {
			_ = cmd.Process.Kill()
		}
	}, func() { _ = windows.CloseHandle(pinned) }
}

func killWindowsTree(cmd *exec.Cmd) {
	// Resolve taskkill from the system directory instead of PATH (or the
	// current directory, which Windows searches first) so a planted
	// taskkill.exe cannot be executed. Without SystemRoot, do not fall back to a
	// PATH lookup; kill the direct child instead.
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		_ = cmd.Process.Kill()
		return
	}
	taskkill := filepath.Join(systemRoot, "System32", "taskkill.exe")
	if _, err := os.Stat(taskkill); err != nil {
		_ = cmd.Process.Kill()
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
