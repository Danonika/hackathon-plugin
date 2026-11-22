//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly && !windows

package filelock

import (
	"errors"
	"os"
)

// On systems without a supported advisory lock, never reclaim a peer's file.
// A crashed owner on these systems requires offline removal of the lock.
func tryLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		return nil, errBusy
	}
	if err != nil {
		return nil, err
	}
	return func() { _ = f.Close(); _ = os.Remove(path) }, nil
}
