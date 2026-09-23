#!/usr/bin/env sh
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
# Canonicalize a symlinked invocation so the root is derived from the real file.
if [ -L "$SELF" ] && command -v readlink >/dev/null 2>&1; then
  TARGET=$(readlink "$SELF" 2>/dev/null || true)
  case "$TARGET" in
    /*) SELF="$TARGET" ;;
    "") : ;;
    *) SELF="$(dirname -- "$SELF")/$TARGET" ;;
  esac
fi
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$SELF")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"

# The launcher execs whatever binary sits under the derived root, so refuse to
# run unless that root really is a Hackathon Agent Kit checkout (guards an unresolved or
# wrong root from a cwd-relative caller).
if [ ! -f "$REPO_ROOT/plugins/hackathonkit/scripts/hooks.js" ] && [ ! -f "$REPO_ROOT/go.mod" ]; then
  echo "Hackathon Agent Kit launcher: '$REPO_ROOT' does not look like the Hackathon Agent Kit repository root" >&2
  exit 1
fi

if [ -f "$REPO_ROOT/bin/hackathonkit-mcp" ] && [ -x "$REPO_ROOT/bin/hackathonkit-mcp" ]; then
  exec "$REPO_ROOT/bin/hackathonkit-mcp" --repo-root "$REPO_ROOT" "$@"
fi

if [ -e "$REPO_ROOT/bin/hackathonkit-mcp" ]; then
  echo "Hackathon Agent Kit launcher: $REPO_ROOT/bin/hackathonkit-mcp exists but is not an executable file" >&2
  exit 1
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
set +e
docker compose -f "$REPO_ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp "$@"
status=$?
set -e
if [ "$status" -ne 0 ]; then
  echo "Hackathon Agent Kit launcher: docker compose run failed with status $status (check the daemon and required .env values)" >&2
fi
exit "$status"
