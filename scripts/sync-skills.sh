#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC="$ROOT/plugins/hackathonkit/skills"
DST="$ROOT/.agents/skills"

if [ ! -d "$SRC" ]; then
  echo "skills source not found: $SRC" >&2
  exit 1
fi

rm -rf "$DST"
mkdir -p "$DST"
cp -R "$SRC/." "$DST/"

COUNT_SRC=$(find "$SRC" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
COUNT_DST=$(find "$DST" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
echo "[sync-skills] mirrored $COUNT_DST of $COUNT_SRC skills to .agents/skills"

if [ "$COUNT_SRC" != "$COUNT_DST" ]; then
  echo "[sync-skills] skill count mismatch" >&2
  exit 1
fi
