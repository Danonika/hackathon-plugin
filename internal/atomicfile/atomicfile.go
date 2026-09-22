// Package atomicfile writes small state files through a unique temporary file
// in the same directory and an atomic rename, so concurrent readers never
// observe a truncated or interleaved file.
package atomicfile

import (
	"os"
	"path/filepath"
	"time"
)

const (
	// ModeContent is for files other tooling and bind-mounted containers read.
	ModeContent os.FileMode = 0o644
	// ModeState is the least-privilege mode for state files (plans, gate
	// reports, approvals) per the security-review checklist.
	ModeState os.FileMode = 0o600
)

// Write writes data to path atomically, creating parent directories as needed.
// The temporary file is chmodded to mode because os.CreateTemp creates it with
// 0o600; callers pass ModeContent for shared content and ModeState for
// least-privilege state files. A zero mode defaults to ModeContent.
func Write(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = ModeContent
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Best-effort: reclaim temp files left by a previous crash.
	if stale, err := filepath.Glob(filepath.Join(dir, filepath.Base(path)+".*.tmp")); err == nil {
		for _, candidate := range stale {
			if info, statErr := os.Stat(candidate); statErr == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(candidate)
			}
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	// Flush before the rename so a crash cannot leave an empty or truncated
	// file behind a valid name.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	if dirHandle, err := os.Open(dir); err == nil {
		_ = dirHandle.Sync()
		_ = dirHandle.Close()
	}
	return nil
}
