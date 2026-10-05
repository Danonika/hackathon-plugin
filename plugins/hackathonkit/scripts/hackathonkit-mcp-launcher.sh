#!/usr/bin/env sh
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *)
    # A slash-less invocation must be resolved to a real path: falling back to
    # the literal name makes dirname return "." and derives the repo root from
    # the caller's cwd (an attacker-controlled directory).
    RESOLVED=$(command -v -- "$SELF" 2>/dev/null || true)
    if [ -z "$RESOLVED" ]; then
      echo "Hackathon Agent Kit launcher: cannot resolve launcher path from '$SELF'" >&2
      exit 1
    fi
    SELF="$RESOLVED"
    ;;
esac
# Resolve symlink chains so the root is derived from the real file. A single
# readlink hop stops at an intermediate link and computes the wrong root.
if command -v realpath >/dev/null 2>&1; then
  SELF=$(realpath -- "$SELF" 2>/dev/null || printf '%s' "$SELF")
elif command -v readlink >/dev/null 2>&1; then
  hops=0
  while [ -L "$SELF" ] && [ "$hops" -lt 40 ]; do
    TARGET=$(readlink "$SELF" 2>/dev/null || true)
    if [ -z "$TARGET" ]; then
      break
    fi
    case "$TARGET" in
      /*) SELF="$TARGET" ;;
      *) SELF="$(dirname -- "$SELF")/$TARGET" ;;
    esac
    hops=$((hops + 1))
  done
fi
SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$SELF")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd -P)"

# The launcher execs whatever binary sits under the derived root, so refuse to
# run unless that root really is a Hackathon Agent Kit checkout (guards an unresolved or
# wrong root from a cwd-relative caller).
if [ ! -f "$REPO_ROOT/plugins/hackathonkit/scripts/hooks.js" ] && [ ! -f "$REPO_ROOT/go.mod" ]; then
  echo "Hackathon Agent Kit launcher: '$REPO_ROOT' does not look like the Hackathon Agent Kit repository root" >&2
  exit 1
fi

# The launcher owns --repo-root; a caller-supplied value would silently win in
# the Go flag parser and could point at a different tree than the docker branch.
for arg in "$@"; do
  # The Go flag parser treats -repo-root and --repo-root as equivalent, so
  # reject the single-dash spelling too or a caller could override the root.
  case "$arg" in
    -repo-root|--repo-root|-repo-root=*|--repo-root=*)
      echo "Hackathon Agent Kit launcher: --repo-root is set by the launcher and must not be passed again" >&2
      exit 1
      ;;
  esac
done

if [ -f "$REPO_ROOT/bin/hackathonkit-mcp" ] && [ -x "$REPO_ROOT/bin/hackathonkit-mcp" ]; then
  exec "$REPO_ROOT/bin/hackathonkit-mcp" --repo-root "$REPO_ROOT" "$@"
fi

if [ -e "$REPO_ROOT/bin/hackathonkit-mcp" ]; then
  # A non-executable local binary (zip/tar extraction, /mnt/c checkout) is
  # recoverable: warn and fall through to the docker fallback.
  echo "Hackathon Agent Kit launcher: $REPO_ROOT/bin/hackathonkit-mcp is not executable; falling back to docker compose" >&2
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
