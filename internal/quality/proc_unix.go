//go:build unix

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

func ownGroup(cmd *exec.Cmd) (kill func(), release func()) {
	if kill, release, ok := kernelGroup(cmd); ok {
		return kill, release
	}

	return pinGroupMember(cmd)
}

func pinGroupMember(cmd *exec.Cmd) (func(), func()) {
	// Systems without process-group descriptors keep a lightweight member of
	// the original group until cleanup. Its pipe closes if the gate process
	// exits. Do not reap that member until the final signal operation finishes:
	// the kernel then cannot reuse the group's number under the cancellation.
	guard := exec.Command("/bin/sh", "-c", "read -r hold")
	guard.Env = []string{}
	guard.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pgid: cmd.Process.Pid}
	pipe, err := guard.StdinPipe()
	if err == nil {
		if err = guard.Start(); err == nil {
			return func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }, func() { _ = pipe.Close(); _ = guard.Wait() }
		}
		_ = pipe.Close()
	}
	// If the group cannot be pinned, target only the directly owned process.
	return func() { _ = cmd.Process.Kill() }, func() {}
}
