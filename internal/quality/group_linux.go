//go:build linux

package quality

import (
	"os/exec"

	"golang.org/x/sys/unix"
)

// Linux 6.9 added PIDFD_SIGNAL_PROCESS_GROUP to pidfd_send_signal(2).
const pidfdSignalProcessGroup = 1 << 2

// Linux 6.9 and later can target a process group by the original leader's
// pidfd even after Wait reaps the leader. Probe support without signaling.
func kernelGroup(cmd *exec.Cmd) (kill func(), release func(), ok bool) {
	fd := -1
	_ = cmd.Process.WithHandle(func(handle uintptr) {
		if unix.PidfdSendSignal(int(handle), 0, nil, pidfdSignalProcessGroup) != nil {
			return
		}
		dup, err := unix.FcntlInt(handle, unix.F_DUPFD_CLOEXEC, 0)
		if err == nil {
			fd = dup
		}
	})
	if fd < 0 {
		return nil, nil, false
	}
	return func() { _ = unix.PidfdSendSignal(fd, unix.SIGKILL, nil, pidfdSignalProcessGroup) }, func() { _ = unix.Close(fd) }, true
}
