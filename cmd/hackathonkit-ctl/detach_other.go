//go:build !unix && !windows

package main

import "syscall"

func detachSysProcAttr() *syscall.SysProcAttr { return nil }
