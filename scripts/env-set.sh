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
  IFS= read -r VALUE || true
fi

MODE="600"
if [ -f "$FILE" ]; then
  MODE=$(stat -c '%a' "$FILE" 2>/dev/null || stat -f '%Lp' "$FILE" 2>/dev/null || echo 600)
fi

TMP="$FILE.tmp.$$"
trap 'rm -f "$TMP"' EXIT INT TERM

if [ ! -f "$FILE" ]; then
  (umask 077 && : > "$FILE")
fi

# The key is matched literally (no regex) and the value travels through the
# environment, so backslashes and other escape sequences survive unchanged.
(umask 077 && KEY="$KEY" VALUE="$VALUE" awk '
  BEGIN { k = ENVIRON["KEY"]; v = ENVIRON["VALUE"]; done = 0 }
  substr($0, 1, length(k) + 1) == k "=" { print k "=" v; done = 1; next }
  { print }
  END { if (!done) print k "=" v }
' "$FILE" > "$TMP")

mv "$TMP" "$FILE"
chmod "$MODE" "$FILE" 2>/dev/null || true
