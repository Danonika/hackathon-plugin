//go:build windows

package quality

import "os/exec"

// Windows has no POSIX process groups here; CommandContext kills the direct
// child and WaitDelay bounds the wait for its pipes.
func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
