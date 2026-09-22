//go:build windows

package main

import "syscall"

// detachedProcess is Windows' DETACHED_PROCESS creation flag, which the
// standard syscall package does not export.
const detachedProcess = 0x00000008

// detachSysProcAttr starts the daemon detached from the parent console on
// Windows (DETACHED_PROCESS) while keeping its own process group.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess}
}
