//go:build windows

package main

import "syscall"

// detachSysProcAttr starts the daemon in a new process group on Windows.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}
