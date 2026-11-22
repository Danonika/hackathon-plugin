# The caller holds the writer lock and owns cleanup of the staging directory.
# Compare without following links. Include file mode as well as bytes, since
# skill bundles can contain executable scripts.
mirrors_equal() (
  left=$1
  right=$2
  (cd "$left" && find . -exec sh -c '
    peer=$1; shift
    for entry do
      other=$peer/$entry
      if [ -L "$entry" ]; then
        [ -L "$other" ] || exit 1
        a=$(readlink "$entry" && printf .) || exit 1
        b=$(readlink "$other" && printf .) || exit 1
        [ "$a" = "$b" ] || exit 1
        continue
      fi
      [ ! -L "$other" ] || exit 1
      if [ -d "$entry" ]; then [ -d "$other" ] || exit 1
      elif [ -f "$entry" ] && [ -f "$other" ]; then
        if command -v cmp >/dev/null 2>&1; then cmp -s "$entry" "$other" || exit 1
        else diff "$entry" "$other" >/dev/null || exit 1; fi
      else exit 1; fi
      a=$(stat -c %a "$entry" 2>/dev/null || stat -f %Lp "$entry") || exit 1
      b=$(stat -c %a "$other" 2>/dev/null || stat -f %Lp "$other") || exit 1
      [ "$a" = "$b" ] || exit 1
    done
  ' sh "$right" {} +) || exit 1
  (cd "$right" && find . -exec sh -c '
    peer=$1; shift
    for entry do [ -e "$peer/$entry" ] || [ -L "$peer/$entry" ] || exit 1; done
  ' sh "$left" {} +)
)

exchange_mirror() {
  command -v python3 >/dev/null 2>&1 || return 2
  python3 - "$1" "$2" <<'PY'
import ctypes, errno, os, sys

libc = ctypes.CDLL(None, use_errno=True)
left, right = map(os.fsencode, sys.argv[1:])
if sys.platform.startswith("linux") and hasattr(libc, "renameat2"):
    result = libc.renameat2(-100, left, -100, right, 2)  # AT_FDCWD, RENAME_EXCHANGE
elif sys.platform == "darwin" and hasattr(libc, "renamex_np"):
    result = libc.renamex_np(left, right, 2)  # RENAME_SWAP
else:
    sys.exit(2)
if result != 0:
    error = ctypes.get_errno()
    if error in (errno.ENOSYS, errno.EINVAL, errno.ENOTSUP):
        sys.exit(2)
    raise OSError(error, os.strerror(error))
PY
}

publish_mirror() (
  stage=$1
  live=$2
  if [ -L "$live" ] || { [ -e "$live" ] && [ ! -d "$live" ]; }; then
    echo "[sync] refusing non-directory mirror: $live" >&2
    exit 1
  fi
  if [ ! -d "$live" ]; then
    mv "$stage" "$live"
    exit
  fi
  if mirrors_equal "$stage" "$live"; then exit 0; fi
  status=0
  exchange_mirror "$stage" "$live" || status=$?
  case "$status" in
    0) exit 0 ;;
    2) ;;
    *) echo "[sync] directory exchange failed for $live" >&2; exit 1 ;;
  esac
  if [ "${HACKATHONKIT_SYNC_OFFLINE:-0}" != 1 ]; then
    echo "[sync] atomic exchange unavailable; kept $live. Set HACKATHONKIT_SYNC_OFFLINE=1 during offline maintenance to replace it." >&2
    exit 1
  fi
  backup="$live.old.$$"
  if [ -e "$backup" ] || [ -L "$backup" ]; then
    echo "[sync] backup already exists: $backup" >&2
    exit 1
  fi
  restore() {
    if [ -d "$backup" ] && [ ! -e "$live" ] && [ ! -L "$live" ]; then
      mv "$backup" "$live" || echo "[sync] restore $live from $backup" >&2
    fi
  }
  trap restore EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  mv "$live" "$backup" || exit 1
  mv "$stage" "$live" || exit 1
  rm -rf "$backup"
)
