// Package atomicfile writes small state files through a unique temporary file
// in the same directory and an atomic rename, so concurrent readers never
// observe a truncated or interleaved file.
package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	// ModeContent is for files other tooling and bind-mounted containers read.
	ModeContent os.FileMode = 0o644
	// ModeState is the least-privilege mode for state files (plans, gate
	// reports, approvals) per the security-review checklist.
	ModeState os.FileMode = 0o600
)

// tempMarker namespaces the temporary files this package creates, so the
// stale-temp reclamation below can never touch a file another tool happens to
// name like ours.
const tempMarker = ".hackathonkit-tmp."

// Write writes data to path atomically, creating parent directories as needed.
// The temporary file is chmodded to mode because os.CreateTemp creates it with
// 0o600; callers pass ModeContent for shared content and ModeState for
// least-privilege state files. A zero mode defaults to ModeContent.
func Write(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = ModeContent
	}
	dir := filepath.Dir(path)
	dirMode := os.FileMode(0o755)
	if mode&0o077 == 0 {
		// Least-privilege state files get a private containing directory.
		dirMode = 0o700
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	// Best-effort: reclaim temp files left by a previous crash. Iterate the
	// directory instead of globbing so metacharacters in the base name cannot
	// widen the match set, and require our unique marker so only files this
	// package created are ever candidates.
	prefix := filepath.Base(path) + tempMarker
	if entries, err := os.ReadDir(dir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".tmp") {
				continue
			}
			info, statErr := entry.Info()
			if statErr == nil && time.Since(info.ModTime()) > time.Hour {
				_ = os.Remove(filepath.Join(dir, name))
			}
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+tempMarker+"*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if mode&0o077 == 0 && runtime.GOOS != "windows" {
		// MkdirAll leaves an existing directory's mode untouched, so tighten it
		// explicitly to keep the private-directory guarantee for state files.
		// Done after CreateTemp so an unwritable directory still fails here.
		if err := os.Chmod(dir, dirMode); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return err
		}
	}
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
	// Sync the directory so the rename itself is durable, not just the file
	// contents. A sync failure means the new name can disappear on a crash, so
	// it must be surfaced instead of silently reported as success.
	dirHandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := dirHandle.Sync(); err != nil {
		dirHandle.Close()
		return err
	}
	return dirHandle.Close()
}
