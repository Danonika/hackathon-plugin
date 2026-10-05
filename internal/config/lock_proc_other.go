//go:build !unix && !windows

package config

// lockProcessAlive is a conservative fallback for platforms without a
// supported process probe: a recorded positive pid is assumed to be alive so a
// stale lock is only reclaimed via the mtime fallback path.
func lockProcessAlive(pid int) bool {
	return pid > 0
}
