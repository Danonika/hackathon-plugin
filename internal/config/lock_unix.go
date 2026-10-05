//go:build unix

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// lockAcquireTimeout bounds how long acquireLock waits for a peer before
// giving up. It mirrors the portable (non-unix) implementation so a stuck peer
// holding the lock after a crash or during slow filesystem I/O cannot wedge a
// caller indefinitely.
const lockAcquireTimeout = 5 * time.Second

// acquireLock takes an exclusive advisory lock on path, serializing the routing
// read-modify-write across processes that share the configs directory. The
// kernel drops the lock automatically if the process dies, so there is no
// stale-lock problem. Acquisition is bounded by lockAcquireTimeout and polls
// with LOCK_NB rather than blocking forever in the kernel.
func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(lockAcquireTimeout)
	for {
		lockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if lockErr != syscall.EWOULDBLOCK && lockErr != syscall.EAGAIN {
			f.Close()
			return nil, lockErr
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("config: timed out acquiring lock %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
