//go:build windows

package config

import (
	"errors"
	"syscall"
)

// processQueryLimitedInformation is PROCESS_QUERY_LIMITED_INFORMATION, which
// the standard syscall package does not export.
const processQueryLimitedInformation = 0x1000

// stillActive is STILL_ACTIVE, returned by GetExitCodeProcess for a live process.
const stillActive = 259

// errorAccessDenied is ERROR_ACCESS_DENIED, returned when the process exists
// but cannot be opened with the requested access.
const errorAccessDenied = 5

// lockProcessAlive reports whether pid is a live process. os.Process.Signal
// only supports os.Kill on Windows, so liveness is probed via the Win32 API.
func lockProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		return errors.Is(err, syscall.Errno(errorAccessDenied))
	}
	defer syscall.CloseHandle(handle)
	var code uint32
	if err := syscall.GetExitCodeProcess(handle, &code); err != nil {
		return false
	}
	return code == stillActive
}
