//go:build !windows && !plan9 && !js && !wasip1

package quality

import (
	"os/exec"
	"syscall"
)

// configureProcessGroup starts each gate command in its own process group so a
// timeout can terminate the whole tree (compilers, shell scripts, grandchildren).
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// Probe liveness first: once Wait has reaped the child, its PID (and hence
	// the process-group id) can be recycled, so a raw group kill could signal
	// an unrelated process group. os.Process.Signal is synchronized with Wait
	// and returns os.ErrProcessDone after reaping.
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		// Best effort: the process may already be gone.
	}
}
