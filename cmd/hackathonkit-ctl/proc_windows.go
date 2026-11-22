//go:build windows

package main

import (
	"errors"
	"math"
	"os"
	"runtime"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

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
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return !errors.Is(err, windows.ERROR_INVALID_PARAMETER)
	}
	defer windows.CloseHandle(handle)
	state, err := windows.WaitForSingleObject(handle, 0)
	return err != nil || state != windows.WAIT_OBJECT_0
}

func processBirth(pid int) string {
	if !validPID(pid) {
		return ""
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var birth, exit, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &birth, &exit, &kernel, &user) != nil {
		return ""
	}
	return strconv.FormatUint(uint64(birth.HighDateTime)<<32|uint64(birth.LowDateTime), 10)
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

// Old PID files lack process creation times. Check the command's actual argv
// before accepting a matching executable shared by several CLI subcommands.
func processLooksLikeDaemon(pid int, recorded string) bool {
	if !validPID(pid) {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var size uint32
	_ = windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, nil, 0, &size)
	if size < uint32(unsafe.Sizeof(windows.NTUnicodeString{})) || size > 1<<17 {
		return false
	}
	buffer := make([]byte, size)
	ptr := unsafe.Pointer(&buffer[0])
	if err := windows.NtQueryInformationProcess(h, windows.ProcessCommandLineInformation, ptr, size, &size); err != nil {
		return false
	}
	args, err := windows.DecomposeCommandLine((*windows.NTUnicodeString)(ptr).String())
	runtime.KeepAlive(buffer)
	return err == nil && legacyDaemonArgs(args, recorded)
}
