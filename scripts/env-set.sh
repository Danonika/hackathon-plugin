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

# Resolve symlink chains iteratively (`readlink -f`/`realpath` are not portable,
# and a single hop would replace an intermediate link with a regular file).
# Refuse to proceed when a link cannot be resolved, so the write never lands on
# the wrong file.
hops=0
while [ -L "$FILE" ]; do
  hops=$((hops + 1))
  if [ "$hops" -gt 40 ]; then
    echo "env-set: too many symlink levels resolving $FILE" >&2
    exit 1
  fi
  TARGET=$(readlink "$FILE" 2>/dev/null) || {
    echo "env-set: cannot resolve symlink $FILE" >&2
    exit 1
  }
  if [ -z "$TARGET" ]; then
    echo "env-set: cannot resolve symlink $FILE" >&2
    exit 1
  fi
  case "$TARGET" in
    /*) FILE="$TARGET" ;;
    *) FILE="$(dirname -- "$FILE")/$TARGET" ;;
  esac
done

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
  # Snapshot the identity of the lock we judged stale, then re-read it after the
  # rename-aside. A peer may have removed and re-created the lock in between; in
  # that case the renamed directory is a live peer's lock and is restored rather
  # than deleted.
  holder=$(cat "$LOCK/pid" 2>/dev/null || true)
  owner=$(cat "$LOCK/owner" 2>/dev/null || true)
  aside="$LOCK.stale.$$"
  if mv "$LOCK" "$aside" 2>/dev/null; then
    now_holder=$(cat "$aside/pid" 2>/dev/null || true)
    now_owner=$(cat "$aside/owner" 2>/dev/null || true)
    if [ "$now_holder" = "$holder" ] && [ "$now_owner" = "$owner" ]; then
      rm -rf "$aside"
    elif [ ! -e "$LOCK" ]; then
      # Break the swap claim and put the still-live lock back.
      mv "$aside" "$LOCK" 2>/dev/null || true
    else
      # Another contender already claimed the path; leave its lock alone and do
      # not delete this one, so both live locks survive.
      :
    fi
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
release_lock() {
  [ "$LOCKED" = "1" ] || return 0
  # Verify ownership, then release with an atomic rename-aside. If the owner file
  # is unreadable (written best-effort), fall back to the pid so a genuine owner
  # still releases; a peer's live lock is unaffected either way.
  owner=$(cat "$LOCK/owner" 2>/dev/null || true)
  pid_owner=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ "$owner" = "$LOCK_TOKEN" ] || { [ -z "$owner" ] && [ "$pid_owner" = "$$" ]; }; then
    if mv "$LOCK" "$LOCK.done.$$" 2>/dev/null; then
      rm -rf "$LOCK.done.$$"
    fi
  fi
  LOCKED=0
}

cleanup_env_set() {
  rm -f "$TMP"
  release_lock
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

# Apply the preserved mode/owner to the temp file before the rename, so the
# install is atomic and the live file never keeps the temp file's 0600 mode.
# Silently downgrading permissions on a credentials file is worse than failing,
# so a mode restore failure is fatal.
if ! chmod "$MODE" "$TMP" 2>/dev/null; then
  echo "env-set: cannot restore mode $MODE on $FILE" >&2
  exit 1
fi
if [ -n "$OWNER" ]; then
  if ! chown "$OWNER" "$TMP" 2>/dev/null; then
    echo "env-set: cannot restore owner $OWNER on $FILE" >&2
    exit 1
  fi
fi
mv "$TMP" "$FILE"
