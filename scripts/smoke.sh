#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DOCKER_BIN="${HACKATHONKIT_SMOKE_DOCKER:-docker}"

REQUESTS=$(cat <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"0"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}
EOF
)

TRANSPORT="auto"
case "${1:-}" in
  ""|--auto) TRANSPORT="auto" ;;
  --binary) TRANSPORT="binary" ;;
  --compose) TRANSPORT="compose" ;;
  *)
    echo "[smoke] unknown argument: $1 (expected --auto, --binary or --compose)" >&2
    exit 2
    ;;
esac

ERR_FILE=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-smoke-stderr.XXXXXX")
cleanup_smoke() { rm -f "$ERR_FILE"; }
trap cleanup_smoke EXIT
trap 'cleanup_smoke; exit 130' INT
trap 'cleanup_smoke; exit 143' TERM

# Bound the MCP invocation so a hanging server fails the smoke test instead of
# stalling CI forever. Both bounds are configurable; a compose run may need to
# pull an image and start containers, so it gets a larger default. When
# GNU timeout is absent, the helper uses gtimeout or Python 3.
SMOKE_TIMEOUT="${HACKATHONKIT_SMOKE_TIMEOUT:-60}"
SMOKE_COMPOSE_TIMEOUT="${HACKATHONKIT_SMOKE_COMPOSE_TIMEOUT:-180}"
. "$ROOT/scripts/bounded.sh"

run_binary() {
  printf '%s\n' "$REQUESTS" | run_with_timeout "$SMOKE_TIMEOUT" "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" 2>"$ERR_FILE"
}

fail_with_stderr() {
  echo "[smoke] $1" >&2
  if [ -s "$ERR_FILE" ]; then
    tail -5 "$ERR_FILE" >&2
  fi
  exit 1
}

run_compose() {
  printf '%s\n' "$REQUESTS" | run_with_timeout "$SMOKE_COMPOSE_TIMEOUT" "$DOCKER_BIN" compose -f "$ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp 2>"$ERR_FILE"
}

case "$TRANSPORT" in
  binary)
    if [ ! -x "$ROOT/bin/hackathonkit-mcp" ]; then
      echo "[smoke] --binary requires $ROOT/bin/hackathonkit-mcp" >&2
      exit 1
    fi
    if ! OUT="$(run_binary)"; then
      fail_with_stderr "local binary failed"
    fi
    ;;
  compose)
    if ! command -v "$DOCKER_BIN" >/dev/null 2>&1; then
      echo "[smoke] --compose requires the docker CLI" >&2
      exit 1
    fi
    if ! OUT="$(run_compose)"; then
      fail_with_stderr "docker compose failed"
    fi
    ;;
  auto)
    if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
      # Sort the candidates so the diagnostic name is deterministic; HACKATHONKIT_SMOKE_SKIP_STALE
      # forces the local binary when the freshness heuristic is not wanted.
      if [ -n "${HACKATHONKIT_SMOKE_SKIP_STALE:-}" ]; then
        NEWEST=""
      else
        # Scan the whole module for every build-affecting input (sources, the
        # module files, Makefile and embedded assets), not just *.go under
        # cmd//internal/, so a go.mod-only or asset change still marks the
        # binary stale.
        NEWEST=$(find "$ROOT" \
            \( -path "$ROOT/.git" -o -path "$ROOT/node_modules" -o -path "$ROOT/data" -o -path "$ROOT/evals" -o -path "$ROOT/plugins" -o -path "$ROOT/bin" -o -path "$ROOT/logs" -o -path "$ROOT/models" \) -prune -o \
            \( -path "$ROOT/cmd/*" -o -path "$ROOT/internal/*" -o -name '*.go' -o -name '*.s' -o -name '*.c' -o -name '*.h' -o -name '*.syso' -o -name 'go.work' -o -name 'go.work.sum' -o -name 'go.mod' -o -name 'go.sum' -o -name 'Makefile' -o -name '*.html' -o -name '*.js' -o -name '*.css' -o -name '*.sql' -o -name '*.tmpl' -o -name '*.tpl' \) -type f -newer "$ROOT/bin/hackathonkit-mcp" -print 2>/dev/null | LC_ALL=C sort | head -n 1)
      fi
      if [ -n "$NEWEST" ] && command -v "$DOCKER_BIN" >/dev/null 2>&1; then
        echo "[smoke] bin/hackathonkit-mcp is older than $NEWEST; using the compose runtime" >&2
        if ! OUT="$(run_compose)"; then
          fail_with_stderr "docker compose failed"
        fi
      else
        if [ -n "$NEWEST" ]; then
          echo "[smoke] warning: bin/hackathonkit-mcp is older than $NEWEST and docker is unavailable; the smoke result may reflect stale sources (rebuild with scripts/repair.sh or pass --binary)" >&2
        fi
        if ! OUT="$(run_binary)"; then
          fail_with_stderr "local binary failed"
        fi
      fi
    elif command -v "$DOCKER_BIN" >/dev/null 2>&1; then
      if ! OUT="$(run_compose)"; then
        fail_with_stderr "docker compose failed"
      fi
    else
      echo "[smoke] no MCP runtime available (build bin/ or start docker)" >&2
      exit 1
    fi
    ;;
esac

LINES=$(printf '%s\n' "$OUT" | grep -c . || true)
if [ "$LINES" != "3" ]; then
  echo "[smoke] MCP handshake FAILED: expected 3 response lines, got $LINES" >&2
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  if ! printf '%s\n' "$OUT" | python3 -c '
import json
import sys

responses = {}
for line in sys.stdin:
    if not line.strip():
        continue
    try:
        obj = json.loads(line)
    except ValueError:
        sys.exit(1)
    if not isinstance(obj, dict):
        sys.exit(1)
    rid = obj.get("id")
    if isinstance(rid, int) and not isinstance(rid, bool):
        responses[rid] = obj
if set(responses) != {1, 2, 3}:
    sys.exit(1)
for rid in (1, 2, 3):
    obj = responses[rid]
    if "error" in obj or not isinstance(obj.get("result"), dict):
        sys.exit(1)
    result = obj["result"]
    if isinstance(result, dict) and result.get("isError"):
        sys.exit(1)
'; then
    echo "[smoke] MCP handshake FAILED: responses must be valid JSON-RPC results with ids 1,2,3" >&2
    exit 1
  fi
elif command -v node >/dev/null 2>&1; then
  printf '%s\n' "$OUT" | node -e '
    const fs = require("fs")
    const rows = fs.readFileSync(0, "utf8").trim().split(/\r?\n/).map(JSON.parse)
    const ids = new Set(rows.map(row => row.id))
    if (![1, 2, 3].every(id => ids.has(id)) ||
        rows.some(row => row.error !== undefined || row.result == null || row.result.isError === true)) process.exit(1)
  ' || { echo "[smoke] invalid JSON-RPC response" >&2; exit 1; }
elif command -v jq >/dev/null 2>&1; then
  printf '%s\n' "$OUT" | jq -se 'map(.id) | sort == [1,2,3]' >/dev/null &&
    printf '%s\n' "$OUT" | jq -se 'all(.[]; (has("error")|not) and (.result|type)=="object" and .result.isError != true)' >/dev/null || {
      echo "[smoke] invalid JSON-RPC response" >&2; exit 1;
    }
else
  echo "[smoke] Python, Node or jq is required to validate JSON-RPC responses" >&2
  exit 2
fi
echo "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
