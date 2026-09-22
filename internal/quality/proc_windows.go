//go:build windows

package quality

import "os/exec"

// Windows has no POSIX process groups here; CommandContext kills only the
// direct child (not its descendants) and WaitDelay bounds the wait for its
// pipes. This is a known divergence from the POSIX build; gate commands are
// direct compilers/scripts, so the residual risk is a stray grandchild on
// timeout.
func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
