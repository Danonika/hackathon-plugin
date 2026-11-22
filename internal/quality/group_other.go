//go:build unix && !linux

package quality

import "os/exec"

func kernelGroup(*exec.Cmd) (func(), func(), bool) { return nil, nil, false }
