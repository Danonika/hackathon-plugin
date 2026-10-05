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

# Build the mirror in a temporary directory and swap it in with a rollback on
# failure. A single cleanup routine owns the temp dir, the backup and the lock,
# and its traps are installed before acquisition, so a signal in any window
# cannot leave the lock (or a partial mirror) behind.
TMP="$DST.tmp.$$"
LOCK="$DST.lock"
LOCKED=0
OLD=""
finish() {
  if [ -d "$OLD" ] && [ ! -d "$DST" ]; then
    if mv "$OLD" "$DST" 2>/dev/null; then
      OLD=""
    else
      echo "[sync-skills] WARNING: could not restore $DST from $OLD; leaving the backup in place" >&2
    fi
  fi
  # Only delete the backup once the live mirror is known to exist, so a failed
  # restore never destroys the only good copy.
  if [ -n "$OLD" ] && [ -d "$DST" ]; then
    rm -rf "$OLD"
  fi
  rm -rf "$TMP"
  if [ "$LOCKED" = "1" ]; then
    rm -rf "$LOCK"
  fi
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

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
# Content verification is required: counts alone would accept a truncated file
# (or a same-count set of different files) as a valid mirror.
if command -v diff >/dev/null 2>&1; then
  if ! diff -r "$SRC" "$TMP" >/dev/null 2>&1; then
    echo "[sync-skills] mirror content differs from the source" >&2
    exit 1
  fi
elif command -v cmp >/dev/null 2>&1; then
  A_LIST=$(cd "$SRC" && find . -type f | LC_ALL=C sort)
  B_LIST=$(cd "$TMP" && find . -type f | LC_ALL=C sort)
  if [ "$A_LIST" != "$B_LIST" ]; then
    echo "[sync-skills] mirror file list differs from the source" >&2
    exit 1
  fi
  cd "$SRC" || exit 1
  for rel in $A_LIST; do
    if ! cmp -s "$SRC/$rel" "$TMP/$rel"; then
      echo "[sync-skills] mirror content differs: $rel" >&2
      exit 1
    fi
  done
else
  echo "[sync-skills] neither diff nor cmp is available; refusing to publish an unverified mirror" >&2
  exit 1
fi

# Serialize concurrent mirror runs so two writers cannot interleave the swap.
if [ -d "$LOCK" ]; then
  HOLDER=$(cat "$LOCK/pid" 2>/dev/null || true)
  if [ -n "$HOLDER" ] && ! kill -0 "$HOLDER" 2>/dev/null; then
    rm -rf "$LOCK"
  fi
fi
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$LOCK" 2>/dev/null; then
    # Track the lock as soon as it exists: a signal before the pid write must
    # still let finish() remove it, otherwise a pid-less lock is never reclaimed
    # (the stale check above only acts when a numeric pid is present).
    LOCKED=1
    if ! printf '%s' "$$" > "$LOCK/pid" 2>/dev/null; then
      echo "[sync-skills] cannot record the lock pid; aborting" >&2
      rm -rf "$LOCK"
      LOCKED=0
      exit 1
    fi
    break
  fi
  sleep 1
done
if [ "$LOCKED" != "1" ]; then
  echo "[sync-skills] another run holds $LOCK; aborting" >&2
  exit 1
fi

OLD="$DST.old.$$"
if [ -d "$DST" ]; then
  mv "$DST" "$OLD"
fi
if ! mv "$TMP" "$DST"; then
  # finish() (EXIT trap) restores $OLD and leaves it in place if it cannot.
  echo "[sync-skills] mirror swap failed; restoring the previous mirror" >&2
  exit 1
fi
rm -rf "$OLD"
OLD=""
LOCKED=0
rm -rf "$LOCK"
trap - EXIT INT TERM
echo "[sync-skills] mirrored $COUNT_DST of $COUNT_SRC skills to .agents/skills"
