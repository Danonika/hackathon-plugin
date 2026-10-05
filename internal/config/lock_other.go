//go:build !unix

package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	lockStaleAfter = 30 * time.Second
)

// acquireLock emulates an advisory lock with an O_EXCL lock file on platforms
// without flock. The file records the owner pid and a random token so a waiter
// can verify the holder is actually gone before reclaiming, and the owner can
// release only the lock it still owns. A stale lock is reclaimed atomically
// (renamed aside before removal) so a concurrent re-acquire is never deleted.
func acquireLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	token := newLockToken()
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			if _, werr := fmt.Fprintf(f, "%d %s\n", os.Getpid(), token); werr != nil {
				f.Close()
				_ = os.Remove(path)
				return nil, werr
			}
			if cerr := f.Close(); cerr != nil {
				_ = os.Remove(path)
				return nil, cerr
			}
			return func() {
				// Only remove the lock this process still owns: it may have
				// been reclaimed and re-acquired by a peer.
				if lockToken(path) == token {
					_ = os.Remove(path)
				}
			}, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		// Check the deadline unconditionally before any reclaim attempt, so a
		// stuck reclaim cannot spin past the documented timeout.
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("config: timed out acquiring lock %s", path)
		}
		if reclaimStaleLock(path) {
			continue
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// reclaimStaleLock reports whether it successfully removed a stale lock. A
// lock is stale only when its recorded owner process is gone, or (for a lock
// with no readable pid) when the file is older than lockStaleAfter.
func reclaimStaleLock(path string) bool {
	pid := lockOwnerPID(path)
	if pid != 0 {
		if lockProcessAlive(pid) {
			return false
		}
	} else if info, err := os.Stat(path); err != nil || time.Since(info.ModTime()) <= lockStaleAfter {
		return false
	}
	return detachAndRemoveLock(path, pid)
}

// detachAndRemoveLock atomically detaches the lock inode and verifies it is
// still the one judged stale before deleting it. If a peer re-created the lock
// between the liveness check and the rename, the detached inode is restored
// instead of deleting a live lock.
func detachAndRemoveLock(path string, stalePID int) bool {
	detached := fmt.Sprintf("%s.stale.%d.%d", path, os.Getpid(), time.Now().UnixNano())
	if err := os.Rename(path, detached); err != nil {
		return false
	}
	if lockOwnerPID(detached) == stalePID {
		_ = os.Remove(detached)
		return true
	}
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		_ = os.Rename(detached, path)
	} else {
		_ = os.Remove(detached)
	}
	return false
}

// lockOwnerPID reads the pid recorded in a lock file, or 0 when it is
// absent/unreadable/malformed.
func lockOwnerPID(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	pid, err := strconv.Atoi(fields[0])
	if err != nil || pid <= 0 {
		return 0
	}
	return pid
}

// lockToken reads the random token recorded in a lock file, or "" when absent.
func lockToken(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func newLockToken() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
