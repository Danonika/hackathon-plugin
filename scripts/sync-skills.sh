#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
. "$ROOT/scripts/skill-profile.sh"
PROFILE="${1:-$(installed_skill_profile "$ROOT")}"
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
[ "$COUNT_SRC" -gt 0 ] && [ -n "$(find "$SRC" -type f -name SKILL.md -print | sed -n '1p')" ] || {
  echo "[sync-skills] refusing empty skill source" >&2; exit 1;
}
mkdir -p "$ROOT/data/locks" "$ROOT/.agents"
. "$ROOT/scripts/file-lock.sh"
acquire_file_lock "$ROOT/data/locks/agents-skills.lock"

# Stage and verify a complete tree before swapping. Restore interrupted swaps
# under the writer lock before constructing the next stage.
for previous in "$DST.old."*; do
  [ -d "$previous" ] || continue
  if [ ! -e "$DST" ] && [ ! -L "$DST" ]; then mv "$previous" "$DST"; fi
done
TMP=$(mktemp -d "$DST.tmp.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# mktemp creates 0700; apply ordinary directory permissions subject to the caller’s umask.
chmod +rwx "$TMP"
cp -RP "$SRC/." "$TMP/"

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
  # Compare by path via find -exec: find passes each path as a whole argument,
  # so spaces/tabs cannot split it. Newline-containing names are rejected, as
  # they cannot be represented unambiguously in the line-based list.
  if ! find "$SRC" -type f -exec sh -c '
      src_root=$1; dst_root=$2
      # Command substitution strips trailing newlines, so append a sentinel and
      # remove it to obtain a literal newline character.
      newline=$(printf "\nX"); newline=${newline%X}
      shift 2
      for src do
        rel=${src#"$src_root"/}
        case "$rel" in
          *"$newline"*) echo "[sync-skills] refusing: source filename contains a newline: $rel" >&2; exit 1 ;;
        esac
        cmp -s "$src" "$dst_root/$rel" || { echo "[sync-skills] mirror content differs: $rel" >&2; exit 1; }
      done
    ' sh "$SRC" "$TMP" {} +; then
    exit 1
  fi
else
  echo "[sync-skills] neither diff nor cmp is available; refusing to publish an unverified mirror" >&2
  exit 1
fi

filter_skill_stage "$TMP" "$PROFILE"
COUNT_DST=$(count_dirs "$TMP")
. "$ROOT/scripts/publish-mirror.sh"
publish_mirror "$TMP" "$DST"
echo "[sync-skills] mirrored $COUNT_DST of $COUNT_SRC skills to .agents/skills"
