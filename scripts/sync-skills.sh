#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC="$ROOT/plugins/hackathonkit/skills"
DST="$ROOT/.agents/skills"

if [ ! -d "$SRC" ]; then
  echo "skills source not found: $SRC" >&2
  exit 1
fi

count_dirs() {
  count=0
  for entry in "$1"/*/; do
    [ -d "$entry" ] || continue
    count=$((count + 1))
  done
  printf '%s' "$count"
}

COUNT_SRC=$(count_dirs "$SRC")

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

COUNT_DST=$(count_dirs "$TMP")
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
LOCKED=0
# Reclaim a lock left by a crashed run (its holder is gone).
if [ -d "$LOCK" ]; then
  HOLDER=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ -n "$HOLDER" ] && ! kill -0 "$HOLDER" 2>/dev/null; then
    rm -rf "$LOCK"
  fi
fi
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$LOCK" 2>/dev/null; then
    printf '%s' "$$" > "$LOCK/pid" 2>/dev/null || true
    LOCKED=1
    break
  fi
  sleep 1
done
if [ "$LOCKED" != "1" ]; then
  echo "[sync-skills] another run holds $LOCK; aborting" >&2
  exit 1
fi

OLD="$DST.old.$$"
finish() {
  if [ -d "$OLD" ] && [ ! -d "$DST" ]; then
    mv "$OLD" "$DST" 2>/dev/null || true
  fi
  rm -rf "$OLD" "$TMP"
  if [ "$LOCKED" = "1" ]; then
    rm -rf "$LOCK"
  fi
}
on_signal() { finish; exit 130; }
trap finish EXIT
trap on_signal INT TERM

if [ -d "$DST" ]; then
  mv "$DST" "$OLD"
fi
if ! mv "$TMP" "$DST"; then
  [ -d "$OLD" ] && mv "$OLD" "$DST"
  exit 1
fi
rm -rf "$OLD"
LOCKED=0
rm -rf "$LOCK"
trap - EXIT INT TERM
echo "[sync-skills] mirrored $COUNT_DST of $COUNT_SRC skills to .agents/skills"
