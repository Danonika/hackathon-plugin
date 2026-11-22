//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package filelock

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func tryLock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, unix.EWOULDBLOCK) {
			return nil, errBusy
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
