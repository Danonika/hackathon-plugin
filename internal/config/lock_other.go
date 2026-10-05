//go:build !unix

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const lockStaleAfter = 30 * time.Second

// acquireLock emulates an advisory lock with an O_EXCL lock file on platforms
// without flock. A lock older than lockStaleAfter is reclaimed after a crash.
func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
			_ = f.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, serr := os.Stat(path); serr == nil && time.Since(info.ModTime()) > lockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("config: timed out acquiring lock %s", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
