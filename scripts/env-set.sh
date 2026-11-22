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
  # Command substitution removes terminal line endings from piped input.
  VALUE=$(cat)
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

# Serialize the complete read/modify/write transaction, including the first creation.
. "$(dirname -- "$0")/file-lock.sh"
acquire_file_lock "$FILE.write-lock"
if [ -L "$FILE" ] || { [ -e "$FILE" ] && [ ! -f "$FILE" ]; }; then
  echo "env-set: refusing non-regular file $FILE" >&2
  exit 1
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
trap 'rm -f "$TMP"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

INPUT="$FILE"
if [ ! -f "$FILE" ]; then
  INPUT=/dev/null
fi

(umask 077 && KEY="$KEY" VALUE="$VALUE" awk '
  function enc(v,    out, i, c) {
    # Default-deny: emit bare only when the value is provably safe to source
    # unquoted. Anything else is quoted and escaped, so shell metacharacters
    # (`;`, `&`, `|`, `<`, `>`, `(`, `)`, `*`, `?`, `!`, `{`, `}`, `~`, ...)
    # cannot execute when the .env file is later sourced.
    if (v == "" || v ~ /^[A-Za-z0-9_.,:\/@%+=-]+$/) {
      return v
    }
    out = "\""
    for (i=1; i<=length(v); i++) {
      c = substr(v,i,1)
      if (c == "\n") out = out "\\n"
      else if (c == "\r") out = out "\\r"
      else if (c == "\\" || c == "\"" || c == "$" || c == "`") out = out "\\" c
      else out = out c
    }
    return out "\""
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
# Symlinks were resolved above, so a directory/FIFO/device here is a real
# non-regular target. `mv` would move the temp file *into* a directory under
# its temp name (reporting success while writing nothing useful), so refuse.
if [ -L "$FILE" ] || { [ -e "$FILE" ] && [ ! -f "$FILE" ]; }; then
  echo "env-set: refusing to replace non-regular file $FILE" >&2
  exit 1
fi
mv "$TMP" "$FILE"
