//go:build windows

package main

import "syscall"

// detachedProcess is Windows' DETACHED_PROCESS creation flag, which the
// standard syscall package does not export.
const detachedProcess = 0x00000008

// detachSysProcAttr starts the daemon detached from the parent console on
// Windows (DETACHED_PROCESS). The child inherits no console, so no console
// control events (Ctrl+C/Ctrl+Break) reach it; CREATE_NEW_PROCESS_GROUP is
// intentionally omitted because process groups are only meaningful for
// console-attached processes and would be inert here.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess}
}
