#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

REQUESTS=$(cat <<'EOF'
{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","clientInfo":{"name":"smoke","version":"0"}}}
{"jsonrpc":"2.0","id":2,"method":"tools/list"}
{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"system.context","arguments":{}}}
EOF
)

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  OUT="$(printf '%s\n' "$REQUESTS" | "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" 2>/dev/null)"
else
  if ! command -v docker >/dev/null 2>&1; then
    echo "[smoke] no MCP runtime available (build bin/ or start docker)" >&2
    exit 1
  fi
  cd "$ROOT"
  OUT="$(printf '%s\n' "$REQUESTS" | docker compose -f "$ROOT/docker-compose.yml" run --rm -T hackathonkit-mcp 2>/dev/null)"
fi

LINES=$(printf '%s\n' "$OUT" | grep -c '"jsonrpc"' || true)
if [ "$LINES" != "3" ]; then
  echo "[smoke] MCP handshake FAILED: expected 3 JSON-RPC responses, got $LINES" >&2
  exit 1
fi
for id in 1 2 3; do
  if ! printf '%s\n' "$OUT" | grep -q "\"id\":$id"; then
    echo "[smoke] MCP handshake FAILED: missing response id=$id" >&2
    exit 1
  fi
done
echo "[smoke] MCP handshake ok: initialize, tools/list, system.context (stdout is JSON-RPC only)"
