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

run_binary() {
  printf '%s\n' "$REQUESTS" | "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" 2>"$ERR_FILE"
}

fail_with_stderr() {
  echo "[smoke] $1" >&2
  if [ -s "$ERR_FILE" ]; then
    tail -5 "$ERR_FILE" >&2
  fi
  exit 1
}

run_compose() {
  printf '%s\n' "$REQUESTS" | "$DOCKER_BIN" compose -f "$ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp 2>"$ERR_FILE"
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
      NEWEST=$(find "$ROOT/cmd" "$ROOT/internal" -name '*.go' -newer "$ROOT/bin/hackathonkit-mcp" 2>/dev/null | head -n 1)
      if [ -n "$NEWEST" ] && command -v "$DOCKER_BIN" >/dev/null 2>&1; then
        echo "[smoke] bin/hackathonkit-mcp is older than $NEWEST; using the compose runtime" >&2
        if ! OUT="$(run_compose)"; then
          fail_with_stderr "docker compose failed"
        fi
      else
        if [ -n "$NEWEST" ]; then
          echo "[smoke] warning: bin/hackathonkit-mcp is older than $NEWEST" >&2
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
    if "error" in obj or "result" not in obj:
        sys.exit(1)
    result = obj["result"]
    if isinstance(result, dict) and result.get("isError"):
        sys.exit(1)
'; then
    echo "[smoke] MCP handshake FAILED: responses must be valid JSON-RPC results with ids 1,2,3" >&2
    exit 1
  fi
else
  GOT_IDS=$(printf '%s\n' "$OUT" | grep -o '"id"[[:space:]]*:[[:space:]]*[0-9]*' | sed 's/.*[^0-9]\([0-9]*\)$/\1/' | sort -u | paste -sd, -)
  if [ "$GOT_IDS" != "1,2,3" ]; then
    echo "[smoke] MCP handshake FAILED: expected response ids 1,2,3, got '$GOT_IDS'" >&2
    exit 1
  fi
  if printf '%s\n' "$OUT" | grep -q '"error"'; then
    echo "[smoke] MCP handshake FAILED: error response detected" >&2
    exit 1
  fi
  if printf '%s\n' "$OUT" | grep -Eq '"isError"[[:space:]]*:[[:space:]]*true'; then
    echo "[smoke] MCP handshake FAILED: tool call reported isError" >&2
    exit 1
  fi
  if ! printf '%s\n' "$OUT" | grep -q '"result"'; then
    echo "[smoke] MCP handshake FAILED: no result payload detected" >&2
    exit 1
  fi
  JSON_LINES=$(printf '%s\n' "$OUT" | grep -c '"jsonrpc"' || true)
  if [ "$JSON_LINES" != "3" ]; then
    echo "[smoke] MCP handshake FAILED: stdout is not JSON-RPC only" >&2
    exit 1
  fi
fi
echo "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
