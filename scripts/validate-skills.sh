#!/usr/bin/env sh
# Validate the skill catalog against docs/SKILL-STANDARD.md.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)"

if [ -f "$ROOT/bin/hackathonkit-mcp" ] && [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --validate-skills
  exit $?
fi

if command -v go >/dev/null 2>&1; then
  cd "$ROOT"
  if ! go run ./cmd/hackathonkit-mcp --repo-root "$ROOT" --validate-skills; then
    echo "[validate-skills] validation failed (go run exit $?)" >&2
    exit 1
  fi
  exit 0
fi

echo "[validate-skills] no runtime available; build one with scripts/setup.sh" >&2
exit 2
