// Package atomicfile writes small state files through a unique temporary file
// in the same directory and an atomic rename, so concurrent readers never
// observe a truncated or interleaved file.
package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
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
	if mode&0o077 == 0 && runtime.GOOS != "windows" && tightenDir(dir) {
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
	// contents. Directory fsync is not portable: Windows cannot flush a
	// read-only directory handle, and some filesystems (NFS/FUSE/network or
	// bind mounts) return EINVAL/ENOTSUP. The rename is already committed and
	// the file was synced above, so treat an unsupported sync as best-effort
	// while still surfacing genuine I/O errors.
	if runtime.GOOS == "windows" {
		return nil
	}
	dirHandle, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := dirHandle.Sync(); err != nil {
		cerr := dirHandle.Close()
		if isDirSyncUnsupported(err) {
			return cerr
		}
		return err
	}
	return dirHandle.Close()
}

// isDirSyncUnsupported reports whether err means the filesystem cannot fsync a
// directory (as opposed to a real I/O failure).
func isDirSyncUnsupported(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP)
}

// tightenDir reports whether dir is safe to chmod to a private mode. It refuses
// "." (returned by filepath.Dir for a path with no directory component) and
// filesystem/volume roots, so this package never changes the mode of the
// process working directory or the filesystem root.
func tightenDir(dir string) bool {
	clean := filepath.Clean(dir)
	if clean == "." {
		return false
	}
	return filepath.Dir(clean) != clean
}
