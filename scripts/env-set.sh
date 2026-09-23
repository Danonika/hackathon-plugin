#!/usr/bin/env sh
# Set a key in an env file.
#
# Usage: env-set.sh <file> <key> [value]
#
# When <value> is omitted it is read from stdin, so secrets never appear in
# the process argv. The file's permission bits are preserved when it already
# exists, and a new file is created with private 0600 permissions. This matters
# for .env files that hold credentials: a naive rewrite under umask 022 would
# downgrade 0600 to 0644.
set -e
FILE="$1"
KEY="$2"
VALUE="${3-}"
FROM_STDIN=0
if [ "$#" -lt 3 ]; then
  FROM_STDIN=1
fi

if [ -z "$FILE" ] || [ -z "$KEY" ]; then
  echo "usage: env-set.sh <file> <key> [value] (value read from stdin when omitted)" >&2
  exit 2
fi

case "$KEY" in
  [A-Za-z_]*)
    case "$KEY" in
      *[!A-Za-z0-9_]*)
        echo "env-set: invalid key '$KEY' (expected [A-Za-z_][A-Za-z0-9_]*)" >&2
        exit 2
        ;;
    esac
    ;;
  *)
    echo "env-set: invalid key '$KEY' (expected [A-Za-z_][A-Za-z0-9_]*)" >&2
    exit 2
    ;;
esac

if [ "$FROM_STDIN" = "1" ]; then
  VALUE=$(cat)
  # A single trailing newline is a line terminator, not part of the value.
  VALUE=${VALUE%"
"}
fi

if [ -L "$FILE" ]; then
  # readlink (without -f) is portable; resolve a relative target against the
  # link's directory so the write lands on the real file.
  TARGET=$(readlink "$FILE" 2>/dev/null || true)
  case "$TARGET" in
    /*) : ;;
    "") : ;;
    *) TARGET="$(dirname -- "$FILE")/$TARGET" ;;
  esac
  if [ -n "$TARGET" ]; then
    FILE="$TARGET"
  fi
fi

MODE="600"
OWNER=""
if [ -f "$FILE" ]; then
  MODE=$(stat -c '%a' "$FILE" 2>/dev/null || stat -f '%Lp' "$FILE" 2>/dev/null || true)
  if [ -z "$MODE" ]; then
    echo "env-set: warning: cannot read the mode of $FILE; defaulting to 600" >&2
    MODE=600
  fi
  OWNER=$(stat -c '%u:%g' "$FILE" 2>/dev/null || stat -f '%u:%g' "$FILE" 2>/dev/null || true)
fi

TMP=$(mktemp "$FILE.tmp.XXXXXX" 2>/dev/null) || {
  echo "env-set: cannot create a temp file next to $FILE" >&2
  exit 1
}
trap 'rm -f "$TMP"' EXIT INT TERM

INPUT="$FILE"
if [ ! -f "$FILE" ]; then
  INPUT=/dev/null
fi

# The key is matched literally (no regex) and the value travels through the
# environment, so backslashes and other escape sequences survive unchanged.
LOCK="$FILE.lock"
LOCKED=0
LOCK_TOKEN="$$.$(date +%s 2>/dev/null || echo 0)"
lock_is_stale() {
  [ -d "$LOCK" ] || return 1
  holder=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ -n "$holder" ]; then
    # kill -0 fails for a dead pid (and, less often, for EPERM on a live one).
    if kill -0 "$holder" 2>/dev/null; then
      return 1
    fi
    return 0
  fi
  # No pid yet: only reclaim after a grace period, so a concurrent
  # mkdir-then-write window is not stolen from a live writer.
  now=$(date +%s 2>/dev/null || echo 0)
  mtime=$(stat -c %Y "$LOCK" 2>/dev/null || stat -f %m "$LOCK" 2>/dev/null || echo "$now")
  [ $((now - mtime)) -ge 30 ]
}
claim_stale_lock() {
  lock_is_stale || return 0
  # Re-check under a single rename: only the contender whose mv succeeds owns
  # the removal, so a live re-acquire cannot be renamed away by a stale verdict.
  if mv "$LOCK" "$LOCK.stale.$$" 2>/dev/null; then
    rm -rf "$LOCK.stale.$$"
  fi
}
claim_stale_lock
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$LOCK" 2>/dev/null; then
    printf '%s' "$$" > "$LOCK/pid" 2>/dev/null || true
    printf '%s' "$LOCK_TOKEN" > "$LOCK/owner" 2>/dev/null || true
    LOCKED=1
    break
  fi
  # A peer may have removed/re-created the lock; re-evaluate staleness each pass.
  claim_stale_lock
  sleep 1
done
if [ "$LOCKED" != "1" ]; then
  echo "env-set: another writer holds $LOCK; aborting" >&2
  exit 1
fi
cleanup_env_set() {
  rm -f "$TMP"
  if [ "$LOCKED" = "1" ]; then
    # Only remove the lock if this run still owns it.
    if [ "$(cat "$LOCK/owner" 2>/dev/null || true)" = "$LOCK_TOKEN" ]; then
      rm -rf "$LOCK"
    fi
  fi
}
on_signal_env_set() { cleanup_env_set; exit 130; }
trap cleanup_env_set EXIT
trap on_signal_env_set INT TERM

(umask 077 && KEY="$KEY" VALUE="$VALUE" awk '
  function enc(v) {
    # Default-deny: emit bare only when the value is provably safe to source
    # unquoted. Anything else is quoted and escaped, so shell metacharacters
    # (`;`, `&`, `|`, `<`, `>`, `(`, `)`, `*`, `?`, `!`, `{`, `}`, `~`, ...)
    # cannot execute when the .env file is later sourced.
    if (v == "" || v ~ /^[A-Za-z0-9_.,:\/@%+=-]+$/) {
      return v
    }
    gsub(/\\/, "\\\\", v)
    gsub(/"/, "\\\"", v)
    gsub(/\n/, "\\n", v)
    gsub(/\r/, "\\r", v)
    gsub(/\$/, "\\$", v)
    gsub(/`/, "\\`", v)
    return "\"" v "\""
  }
  BEGIN { k = ENVIRON["KEY"]; v = ENVIRON["VALUE"]; done = 0 }
  substr($0, 1, length(k) + 1) == k "=" {
    if (done == 0) {
      print k "=" enc(v)
      done = 1
    }
    next
  }
  { print }
  END { if (!done) print k "=" enc(v) }
' "$INPUT" > "$TMP")

mv "$TMP" "$FILE"
if ! chmod "$MODE" "$FILE" 2>/dev/null; then
  echo "env-set: warning: could not restore mode $MODE on $FILE" >&2
fi
if [ -n "$OWNER" ]; then
  if ! chown "$OWNER" "$FILE" 2>/dev/null; then
    echo "env-set: warning: could not restore owner $OWNER on $FILE" >&2
  fi
fi
