//go:build windows

package main

import "syscall"

// detachSysProcAttr starts the daemon detached from the parent console on
// Windows (DETACHED_PROCESS) while keeping its own process group.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008}
}
