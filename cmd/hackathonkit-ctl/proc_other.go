//go:build !unix && !windows

package main

import (
	"errors"
	"os"
)

// These systems do not expose a supported process identity check.
func processAlive(pid int) bool                        { return pid > 0 }
func processMatchesPath(pid int, recorded string) bool { return false }
func terminateProcess(p *os.Process) error {
	return errors.New("daemon termination unsupported on this platform")
}

func processBirth(pid int) string { return "" }

func processLooksLikeDaemon(pid int, recorded string) bool { return false }
