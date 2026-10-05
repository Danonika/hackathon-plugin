//go:build plan9 || js || wasip1

package quality

import "os/exec"

// These targets have neither POSIX process groups nor Windows job objects.
// Define the hooks so the package still builds; the command is only killed
// directly.
func configureProcessGroup(cmd *exec.Cmd) {}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
