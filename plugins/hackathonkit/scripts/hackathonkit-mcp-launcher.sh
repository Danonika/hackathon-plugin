#!/usr/bin/env sh
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$SELF")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

if [ -x "$REPO_ROOT/bin/hackathonkit-mcp" ]; then
  exec "$REPO_ROOT/bin/hackathonkit-mcp" --repo-root "$REPO_ROOT"
fi

if [ ! -f "$REPO_ROOT/docker-compose.yml" ]; then
  echo "Hackathon Agent Kit launcher: docker-compose.yml not found at $REPO_ROOT" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Hackathon Agent Kit launcher: docker CLI not found and no local binary at $REPO_ROOT/bin/hackathonkit-mcp" >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "Hackathon Agent Kit launcher: docker compose (v2) is unavailable" >&2
  exit 1
fi

cd "$REPO_ROOT"
exec docker compose -f "$REPO_ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp
