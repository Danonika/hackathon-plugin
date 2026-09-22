//go:build windows

package main

import (
	"os"
	"syscall"
)

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, which
// the standard syscall package does not export.
const processQueryLimitedInformation = 0x1000

// stillActive is STILL_ACTIVE, returned by GetExitCodeProcess for a live process.
const stillActive = 259

// processAlive reports whether pid is an existing process. os.Process.Signal
// only supports os.Kill on Windows, so liveness is probed via the Win32 API.
func processAlive(pid int) bool {
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return false
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
