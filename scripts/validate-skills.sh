#!/usr/bin/env sh
# Validate the skill catalog against docs/SKILL-STANDARD.md.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)" || {
  echo "[validate-skills] cannot resolve the repository root" >&2
  exit 1
}
if [ ! -f "$ROOT/docker-compose.yml" ]; then
  echo "[validate-skills] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi

if [ -f "$ROOT/bin/hackathonkit-mcp" ] && [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --validate-skills || exit $?
  exit 0
fi

if command -v go >/dev/null 2>&1; then
  cd "$ROOT" || { echo "[validate-skills] cannot enter $ROOT" >&2; exit 1; }
  if go run ./cmd/hackathonkit-mcp --repo-root "$ROOT" --validate-skills; then
    :
  else
    status=$?
    echo "[validate-skills] validation failed (go run exit $status)" >&2
    exit 1
  fi
  exit 0
fi

echo "[validate-skills] no runtime available; build one with scripts/setup.sh" >&2
exit 2
