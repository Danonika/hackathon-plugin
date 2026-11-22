# Source this helper from sh. Descriptor 9 holds the lock until the caller
# exits, including abnormal exits. Keep the inode in place for future waiters.
acquire_file_lock() {
  if [ -L "$1" ] || { [ -e "$1" ] && [ ! -f "$1" ]; }; then
    echo "refusing non-regular lock file: $1" >&2
    return 1
  fi
  exec 9>>"$1"
  if command -v flock >/dev/null 2>&1; then
    flock -x -w 10 9 && return 0
  elif command -v perl >/dev/null 2>&1; then
    # macOS ships Perl with the same advisory flock(2) primitive.
    perl -MFcntl=:flock -e 'open my $fd, ">&=", 9 or exit 1; alarm 10; flock $fd, LOCK_EX or exit 1' && return 0
  else
    echo "flock or Perl is required for safe concurrent writes" >&2
    return 1
  fi
  echo "could not acquire lock: $1" >&2
  return 1
}
