// Package filelock serializes local file updates across processes.
package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Acquire waits at most timeout for exclusive access. On supported desktop
// platforms the OS releases the lock when the owning handle or process exits.
func Acquire(path string, timeout time.Duration) (func(), error) {
	return AcquireContext(context.Background(), path, timeout)
}

func AcquireContext(ctx context.Context, path string, timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, err := tryLock(path)
		if err == nil {
			return unlock, nil
		}
		if !errors.Is(err, errBusy) {
			return nil, fmt.Errorf("lock %s: %w", path, err)
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out acquiring lock %s: %w", path, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

var errBusy = errors.New("lock held")
