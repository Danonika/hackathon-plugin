package filelock

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestExclusiveLockAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locks", "state.lock")
	release, err := Acquire(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := AcquireContext(ctx, path, time.Second); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("waiting lock returned %v", err)
	}
	release()
	release, err = Acquire(path, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
