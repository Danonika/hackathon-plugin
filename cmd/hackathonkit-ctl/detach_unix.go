//go:build unix

package main

import "syscall"

// detachSysProcAttr starts the daemon in its own session on POSIX systems.
func detachSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
