#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC="$ROOT/plugins/hackathonkit/skills"
DST="$ROOT/.agents/skills"

if [ ! -d "$SRC" ]; then
  echo "skills source not found: $SRC" >&2
  exit 1
fi

COUNT_SRC=$(find "$SRC" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')

# Build the mirror in a temporary directory and swap it in, so consumers never
# observe a missing or half-populated mirror when the copy fails.
TMP="$DST.tmp.$$"
trap 'rm -rf "$TMP"' EXIT INT TERM
rm -rf "$TMP"
mkdir -p "$TMP"
cp -R "$SRC/." "$TMP/"

COUNT_DST=$(find "$TMP" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
if [ "$COUNT_SRC" != "$COUNT_DST" ]; then
  echo "[sync-skills] skill count mismatch: $COUNT_SRC source, $COUNT_DST copied" >&2
  exit 1
fi

OLD="$DST.old.$$"
if [ -d "$DST" ]; then
  mv "$DST" "$OLD"
fi
if ! mv "$TMP" "$DST"; then
  [ -d "$OLD" ] && mv "$OLD" "$DST"
  exit 1
fi
rm -rf "$OLD"
trap - EXIT INT TERM
echo "[sync-skills] mirrored $COUNT_DST of $COUNT_SRC skills to .agents/skills"
