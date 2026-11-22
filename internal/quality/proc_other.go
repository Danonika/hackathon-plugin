//go:build !unix && !windows

package quality

import "os/exec"

// These targets have neither POSIX process groups nor Windows job objects.
// Define the hooks so the package still builds; the command is only killed
// directly.
func configureProcessGroup(cmd *exec.Cmd) {}

func ownGroup(cmd *exec.Cmd) (func(), func()) { return func() { _ = cmd.Process.Kill() }, func() {} }
