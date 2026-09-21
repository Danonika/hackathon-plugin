#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

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

run_binary() {
  printf '%s\n' "$REQUESTS" | "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" 2>/dev/null
}

run_compose() {
  cd "$ROOT"
  printf '%s\n' "$REQUESTS" | docker compose -f "$ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp 2>/dev/null
}

case "$TRANSPORT" in
  binary)
    if [ ! -x "$ROOT/bin/hackathonkit-mcp" ]; then
      echo "[smoke] --binary requires $ROOT/bin/hackathonkit-mcp" >&2
      exit 1
    fi
    OUT="$(run_binary)"
    ;;
  compose)
    if ! command -v docker >/dev/null 2>&1; then
      echo "[smoke] --compose requires the docker CLI" >&2
      exit 1
    fi
    OUT="$(run_compose)"
    ;;
  auto)
    if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
      OUT="$(run_binary)"
    elif command -v docker >/dev/null 2>&1; then
      OUT="$(run_compose)"
    else
      echo "[smoke] no MCP runtime available (build bin/ or start docker)" >&2
      exit 1
    fi
    ;;
esac

LINES=$(printf '%s\n' "$OUT" | grep -c '"jsonrpc"' || true)
if [ "$LINES" != "3" ]; then
  echo "[smoke] MCP handshake FAILED: expected 3 JSON-RPC responses, got $LINES" >&2
  exit 1
fi

if command -v python3 >/dev/null 2>&1; then
  if ! printf '%s\n' "$OUT" | python3 -c '
import json
import sys

expected = [1, 2, 3]
lines = [line for line in sys.stdin if line.strip()]
if len(lines) != 3:
    sys.exit(1)
for line, want in zip(lines, expected):
    try:
        obj = json.loads(line)
    except ValueError:
        sys.exit(1)
    if obj.get("id") != want:
        sys.exit(1)
    if "error" in obj:
        sys.exit(1)
    if "result" not in obj:
        sys.exit(1)
    result = obj.get("result")
    if isinstance(result, dict) and result.get("isError"):
        sys.exit(1)
'; then
    echo "[smoke] MCP handshake FAILED: responses must be valid JSON-RPC results with ids 1,2,3" >&2
    exit 1
  fi
else
  GOT_IDS=$(printf '%s\n' "$OUT" | grep -o '"id":[0-9]*' | sed 's/"id"://' | paste -sd, -)
  if [ "$GOT_IDS" != "1,2,3" ]; then
    echo "[smoke] MCP handshake FAILED: expected response ids 1,2,3 in order, got '$GOT_IDS'" >&2
    exit 1
  fi
  if printf '%s\n' "$OUT" | grep -q '"error"'; then
    echo "[smoke] MCP handshake FAILED: error response detected" >&2
    exit 1
  fi
  if printf '%s\n' "$OUT" | grep -q '"isError":true'; then
    echo "[smoke] MCP handshake FAILED: tool call reported isError" >&2
    exit 1
  fi
  if ! printf '%s\n' "$OUT" | grep -q '"result"'; then
    echo "[smoke] MCP handshake FAILED: no result payload detected" >&2
    exit 1
  fi
fi
echo "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
