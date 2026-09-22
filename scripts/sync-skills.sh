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
cleanup_tmp() { rm -rf "$TMP"; }
on_signal() { cleanup_tmp; exit 130; }
trap cleanup_tmp EXIT
trap on_signal INT TERM
rm -rf "$TMP"
mkdir -p "$TMP"
cp -R "$SRC/." "$TMP/"

COUNT_DST=$(find "$TMP" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
FILES_SRC=$(find "$SRC" -type f | wc -l | tr -d ' ')
FILES_DST=$(find "$TMP" -type f | wc -l | tr -d ' ')
SKILLS_SRC=$(find "$SRC" -name SKILL.md | wc -l | tr -d ' ')
SKILLS_DST=$(find "$TMP" -name SKILL.md | wc -l | tr -d ' ')
if [ "$COUNT_SRC" != "$COUNT_DST" ] || [ "$FILES_SRC" != "$FILES_DST" ] || [ "$SKILLS_SRC" != "$SKILLS_DST" ]; then
  echo "[sync-skills] mirror mismatch: dirs $COUNT_SRC/$COUNT_DST files $FILES_SRC/$FILES_DST SKILL.md $SKILLS_SRC/$SKILLS_DST" >&2
  exit 1
fi

# Serialize concurrent mirror runs so two writers cannot interleave the swap.
LOCK="$DST.lock"
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$LOCK" 2>/dev/null; then
    break
  fi
  sleep 0.2
done
trap 'rm -rf "$LOCK"' EXIT INT TERM

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
