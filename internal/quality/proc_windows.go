//go:build windows

package quality

import (
	"os/exec"
	"strconv"
	"syscall"
)

// configureProcessGroup starts each gate command in a new process group so the
// whole tree can be terminated with taskkill /T.
func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	// taskkill /T terminates the process tree (test binaries, shell children),
	// which WaitDelay alone does not reap. Fall back to the direct child.
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err != nil {
		_ = cmd.Process.Kill()
	}
}
