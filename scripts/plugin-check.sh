#!/usr/bin/env sh
# Verify that the installed Hackathon Agent Kit plugin starts its MCP server from a
# foreign working directory. Exit codes: 0 ok, 1 failed, 2 skipped.
set -e

if ! command -v codex >/dev/null 2>&1; then
  echo "[plugin-check] codex CLI is unavailable; skipping" >&2
  exit 2
fi
JSON="$(codex plugin list --json 2>/dev/null || true)"
if [ -z "$JSON" ]; then
  echo "[plugin-check] codex plugin list is unavailable" >&2
  exit 1
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "[plugin-check] python3 is required for this check; skipping" >&2
  exit 2
fi

printf '%s' "$JSON" | python3 -c '
import json
import os
import subprocess
import sys
import tempfile

raw = sys.stdin.read()
try:
    data = json.loads(raw)
except ValueError:
    print("[plugin-check] cannot parse codex plugin list output", file=sys.stderr)
    sys.exit(1)

entry = None
for plugin in data.get("installed", []):
    if plugin.get("name") == "hackathonkit":
        entry = plugin
        break
if not entry:
    print("[plugin-check] hackathonkit plugin is not installed", file=sys.stderr)
    sys.exit(1)

source = (entry.get("source") or {}).get("path") or ""
if not source:
    print("[plugin-check] installed plugin has no source.path; re-run scripts/setup.sh", file=sys.stderr)
    sys.exit(1)
mcp_path = os.path.join(source, ".mcp.json")
if not os.path.isfile(mcp_path):
    print("[plugin-check] .mcp.json is missing in the installed plugin; run scripts/setup.sh", file=sys.stderr)
    sys.exit(1)
try:
    with open(mcp_path, "r", encoding="utf-8-sig") as fh:
        manifest = json.load(fh)
except (OSError, ValueError) as exc:
    print("[plugin-check] cannot read .mcp.json: %s" % exc, file=sys.stderr)
    sys.exit(1)

server = (manifest.get("mcpServers") or {}).get("hackathonkit")
if not server or not server.get("command"):
    print("[plugin-check] hackathonkit server is missing in .mcp.json", file=sys.stderr)
    sys.exit(1)

requests = "\n".join([
    json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize",
                "params": {"protocolVersion": "2025-06-18",
                           "clientInfo": {"name": "plugin-check", "version": "0"}}}),
    json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
    json.dumps({"jsonrpc": "2.0", "id": 3, "method": "tools/call",
                "params": {"name": "system.context", "arguments": {}}}),
]) + "\n"

cmd = [str(server["command"])] + [str(arg) for arg in (server.get("args") or [])]
with tempfile.TemporaryDirectory(prefix="hackathonkit-plugin-check-") as tmp:
    try:
        proc = subprocess.run(cmd, input=requests, text=True, capture_output=True, cwd=tmp, timeout=120)
    except subprocess.TimeoutExpired:
        print("[plugin-check] FAIL: MCP command timed out after 120s", file=sys.stderr)
        sys.exit(1)
    except OSError as exc:
        print("[plugin-check] FAIL: cannot start MCP command: %s" % exc, file=sys.stderr)
        sys.exit(1)

stderr_tail = "\n".join(line for line in (proc.stderr or "").splitlines()[-5:] if line.strip())
lines = [line for line in proc.stdout.splitlines() if line.strip()]
if len(lines) != 3:
    message = "[plugin-check] FAIL: expected 3 JSON-RPC responses from a foreign working directory, got %d" % len(lines)
    if stderr_tail:
        message += "\n[plugin-check] server stderr:\n" + stderr_tail
    print(message, file=sys.stderr)
    sys.exit(1)
for line, want in zip(lines, [1, 2, 3]):
    try:
        obj = json.loads(line)
    except ValueError:
        print("[plugin-check] FAIL: non-JSON output detected: %s" % line, file=sys.stderr)
        sys.exit(1)
    if obj.get("error"):
        message = (obj.get("error") or {}).get("message", "unknown error")
        print("[plugin-check] FAIL: error response for id=%s: %s" % (want, message), file=sys.stderr)
        sys.exit(1)
    if obj.get("id") != want or "result" not in obj:
        print("[plugin-check] FAIL: unexpected response for id=%s" % want, file=sys.stderr)
        sys.exit(1)
    result = obj.get("result")
    if isinstance(result, dict) and result.get("isError"):
        print("[plugin-check] FAIL: tool call id=%s reported isError" % want, file=sys.stderr)
        sys.exit(1)

print("[plugin-check] ok: installed plugin starts the MCP from a foreign working directory")
'
