#!/usr/bin/env sh
# Set a key in an env file.
#
# Usage: env-set.sh <file> <key> <value>
#
# The file's permission bits are preserved when it already exists, and a new
# file is created with private 0600 permissions. This matters for .env files
# that hold credentials: a naive rewrite under umask 022 would downgrade 0600
# to 0644.
set -e
FILE="$1"
KEY="$2"
VALUE="$3"

if [ -z "$FILE" ] || [ -z "$KEY" ]; then
  echo "usage: env-set.sh <file> <key> <value>" >&2
  exit 2
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

(umask 077 && awk -v k="$KEY" -v v="$VALUE" '
  BEGIN { done = 0 }
  $0 ~ "^" k "=" { print k "=" v; done = 1; next }
  { print }
  END { if (!done) print k "=" v }
' "$FILE" > "$TMP")

mv "$TMP" "$FILE"
chmod "$MODE" "$FILE" 2>/dev/null || true
