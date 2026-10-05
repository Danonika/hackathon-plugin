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
)

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, which
// the standard syscall package does not export.
const processQueryLimitedInformation = 0x1000

// stillActive is STILL_ACTIVE, returned by GetExitCodeProcess for a live process.
const stillActive = 259

// configureProcessGroup starts each gate command in a new process group so the
// whole tree can be terminated with taskkill /T.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// processAlive reports whether pid is a live process. os.Process.Signal only
// supports os.Kill on Windows, so liveness is probed via the Win32 API.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// ERROR_ACCESS_DENIED means the process exists but cannot be queried;
		// only a nonexistent pid is not alive. Other errors fail conservative,
		// treating the process as dead rather than killing an unrelated one.
		return err == syscall.ERROR_ACCESS_DENIED
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// taskkill /T terminates the process tree (test binaries, shell children),
	// which WaitDelay alone does not reap. Probe liveness first: once Wait has
	// reaped the child its PID can be recycled, so taskkill must not target an
	// unrelated process that reused it. Fall back to the direct child.
	if !processAlive(cmd.Process.Pid) {
		return
	}
	// Resolve taskkill from the system directory instead of PATH (or the
	// current directory, which Windows searches first) so a planted
	// taskkill.exe cannot be executed.
	taskkill := filepath.Join(os.Getenv("SystemRoot"), "System32", "taskkill.exe")
	if _, err := os.Stat(taskkill); err != nil {
		taskkill = "taskkill"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, taskkill, "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
