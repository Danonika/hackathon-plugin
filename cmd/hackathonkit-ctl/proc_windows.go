//go:build windows

package main

import (
	"errors"
	"math"
	"os"
	"syscall"

	"golang.org/x/sys/windows"
)

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, which
// the standard syscall package does not export.
const processQueryLimitedInformation = 0x1000

// stillActive is STILL_ACTIVE, returned by GetExitCodeProcess for a live process.
const stillActive = 259

// validPID rejects values that cannot be represented as a Win32 process id, so
// a corrupt pid file cannot be folded onto an unrelated live pid.
func validPID(pid int) bool {
	return pid > 0 && uint64(pid) <= math.MaxUint32
}

// processAlive reports whether pid is an existing process. os.Process.Signal
// only supports os.Kill on Windows, so liveness is probed via the Win32 API.
func processAlive(pid int) bool {
	if !validPID(pid) {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// ERROR_ACCESS_DENIED means the process exists but cannot be queried;
		// only a nonexistent pid (ERROR_INVALID_PARAMETER) is not alive.
		return errors.Is(err, windows.ERROR_ACCESS_DENIED)
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}

// terminateProcess stops the process. Windows has no SIGTERM, so this is a
// hard terminate.
func terminateProcess(process *os.Process) error {
	return process.Kill()
}

// processMatchesPath reports whether the running process at pid is the recorded
// executable, using the Win32 image name.
func processMatchesPath(pid int, recorded string) bool {
	if !validPID(pid) {
		return false
	}
	if pid == os.Getpid() {
		if self, err := os.Executable(); err == nil {
			return samePath(self, recorded)
		}
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	// QueryFullProcessImageName returns ERROR_INSUFFICIENT_BUFFER for paths
	// longer than MAX_PATH; grow and retry.
	buf := make([]uint16, windows.MAX_PATH)
	for {
		size := uint32(len(buf))
		err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size)
		if err == nil {
			return samePath(windows.UTF16ToString(buf[:size]), recorded)
		}
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) && len(buf) < 1<<15 {
			buf = make([]uint16, len(buf)*2)
			continue
		}
		return false
	}
}
