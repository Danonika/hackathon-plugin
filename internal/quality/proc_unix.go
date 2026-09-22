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
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
