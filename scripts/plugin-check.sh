#!/usr/bin/env sh
# Verify that the installed Hackathon Agent Kit plugin starts its MCP server from a
# foreign working directory. Exit codes: 0 ok, 1 failed, 2 skipped.
set -e

if ! command -v codex >/dev/null 2>&1; then
  echo "[plugin-check] codex CLI is unavailable; skipping" >&2
  exit 2
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "[plugin-check] python3 is required for this check; skipping" >&2
  exit 2
fi

JSON_FILE=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-plugin-list.XXXXXX")
cleanup_plugin_check() { rm -f "$JSON_FILE" "$ERR_FILE"; }
trap cleanup_plugin_check EXIT
trap 'cleanup_plugin_check; exit 130' INT
trap 'cleanup_plugin_check; exit 143' TERM
ERR_FILE=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-plugin-list-err.XXXXXX")
if codex plugin list --json >"$JSON_FILE" 2>"$ERR_FILE"; then
  :
else
  status=$?
  echo "[plugin-check] codex plugin list failed (exit $status):" >&2
  tail -3 "$ERR_FILE" >&2 || true
  exit 1
fi

python3 - "$JSON_FILE" <<'PY'
import json
import os
import stat
import subprocess
import sys
import tempfile

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as fh:
    raw = fh.read()
try:
    data = json.loads(raw)
except ValueError as exc:
    print("[plugin-check] cannot parse codex plugin list output: %s" % exc, file=sys.stderr)
    sys.exit(1)
if not isinstance(data, dict) or not isinstance(data.get("installed"), list):
    print("[plugin-check] unexpected codex plugin list shape (expected an 'installed' array)", file=sys.stderr)
    sys.exit(1)

entry = None
for plugin in data["installed"]:
    if isinstance(plugin, dict) and plugin.get("name") == "hackathonkit":
        entry = plugin
        break
if entry is None:
    print("[plugin-check] hackathonkit plugin is not installed", file=sys.stderr)
    sys.exit(1)

source = ""
if isinstance(entry.get("source"), dict):
    source = entry["source"].get("path") or ""
source = os.path.realpath(os.path.expanduser(str(source))) if source else ""
if not source:
    print("[plugin-check] installed plugin has no source.path; re-run scripts/setup.sh", file=sys.stderr)
    sys.exit(1)

# The manifest is about to be executed: refuse a symlinked or group/world-
# writable plugin tree (another user could replace what we run).
try:
    st = os.lstat(source)
    if stat.S_ISLNK(st.st_mode):
        print("[plugin-check] refusing to execute a symlinked plugin directory: %s" % source, file=sys.stderr)
        sys.exit(1)
    if st.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        print("[plugin-check] refusing to execute a group/world-writable plugin directory: %s" % source, file=sys.stderr)
        sys.exit(1)
except OSError as exc:
    print("[plugin-check] cannot inspect the plugin directory: %s" % exc, file=sys.stderr)
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
if not isinstance(manifest, dict) or not isinstance(manifest.get("mcpServers"), dict):
    print("[plugin-check] .mcp.json has no mcpServers object", file=sys.stderr)
    sys.exit(1)

server = manifest["mcpServers"].get("hackathonkit")
if not isinstance(server, dict) or not isinstance(server.get("command"), str) or not server["command"]:
    print("[plugin-check] hackathonkit server is missing in .mcp.json", file=sys.stderr)
    sys.exit(1)
args = server.get("args") or []
if not isinstance(args, list):
    print("[plugin-check] hackathonkit server args must be an array", file=sys.stderr)
    sys.exit(1)

requests = "\n".join([
    json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize",
                "params": {"protocolVersion": "2025-06-18",
                           "clientInfo": {"name": "plugin-check", "version": "0"}}}),
    json.dumps({"jsonrpc": "2.0", "method": "notifications/initialized"}),
    json.dumps({"jsonrpc": "2.0", "id": 2, "method": "tools/list"}),
    json.dumps({"jsonrpc": "2.0", "id": 3, "method": "tools/call",
                "params": {"name": "system.context", "arguments": {}}}),
]) + "\n"

command = server["command"]
if not os.path.isabs(command):
    # A relative command must resolve against the plugin, not the temp cwd.
    command = os.path.join(source, command)
cmd = [command] + [str(arg) for arg in args]
with tempfile.TemporaryDirectory(prefix="hackathonkit-plugin-check-") as tmp:
    try:
        proc = subprocess.run(cmd, input=requests, text=True, errors="replace",
                              capture_output=True, cwd=tmp, timeout=120)
    except subprocess.TimeoutExpired:
        print("[plugin-check] FAIL: MCP command timed out after 120s", file=sys.stderr)
        sys.exit(1)
    except OSError as exc:
        print("[plugin-check] FAIL: cannot start MCP command: %s" % exc, file=sys.stderr)
        sys.exit(1)

stderr_tail = "\n".join(line for line in (proc.stderr or "").splitlines()[-5:] if line.strip())

# Correlate responses by id; servers may interleave notifications or logs.
responses = {}
for line in (proc.stdout or "").splitlines():
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
    except ValueError:
        continue
    rid = obj.get("id") if isinstance(obj, dict) else None
    if isinstance(rid, int) and not isinstance(rid, bool) and rid in (1, 2, 3):
        responses[rid] = obj
    if isinstance(rid, int) and not isinstance(rid, bool) and rid in (1, 2, 3) and obj.get("error"):
        message = (obj.get("error") or {}).get("message", "unknown error")
        print("[plugin-check] FAIL: error response for id=%s: %s" % (obj["id"], message), file=sys.stderr)
        sys.exit(1)

missing = [i for i in (1, 2, 3) if i not in responses]
if missing:
    message = "[plugin-check] FAIL: missing JSON-RPC responses for id(s) %s from a foreign working directory" % missing
    if stderr_tail:
        message += "\n[plugin-check] server stderr:\n" + stderr_tail
    print(message, file=sys.stderr)
    sys.exit(1)

for want in (1, 2, 3):
    obj = responses[want]
    # 'result' may legitimately be falsy (an empty object or false), so the
    # key must exist rather than be truthy.
    if "result" not in obj:
        print("[plugin-check] FAIL: response for id=%s has no result" % want, file=sys.stderr)
        sys.exit(1)
    result = obj["result"]
    if isinstance(result, dict) and result.get("isError"):
        print("[plugin-check] FAIL: tool call id=%s reported isError" % want, file=sys.stderr)
        sys.exit(1)

print("[plugin-check] ok: installed plugin starts the MCP from a foreign working directory")
PY
