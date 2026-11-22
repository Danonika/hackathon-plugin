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

JSON_FILE=""
ERR_FILE=""
cleanup_plugin_check() {
  if [ -n "$JSON_FILE" ]; then rm -f "$JSON_FILE"; fi
  if [ -n "$ERR_FILE" ]; then rm -f "$ERR_FILE"; fi
}
trap cleanup_plugin_check EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
JSON_FILE=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-plugin-list.XXXXXX")
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
import shutil
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
source_raw = os.path.expanduser(str(source)) if source else ""
if not source_raw:
    print("[plugin-check] installed plugin has no source.path; re-run scripts/setup.sh", file=sys.stderr)
    sys.exit(1)

# The manifest is about to be executed: refuse a symlinked or group/world-
# writable plugin tree (another user could replace what we run). Inspect the
# unresolved path first: realpath() resolves the final component, which would
# make an S_ISLNK check dead code.
try:
    st = os.lstat(source_raw)
    if stat.S_ISLNK(st.st_mode):
        print("[plugin-check] refusing to execute a symlinked plugin directory: %s" % source_raw, file=sys.stderr)
        sys.exit(1)
    if st.st_uid not in (os.getuid(), 0) or st.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
        print("[plugin-check] refusing to execute a group/world-writable plugin directory: %s" % source_raw, file=sys.stderr)
        sys.exit(1)
except OSError as exc:
    print("[plugin-check] cannot inspect the plugin directory: %s" % exc, file=sys.stderr)
    sys.exit(1)

source = os.path.realpath(source_raw)

mcp_path = os.path.join(source, ".mcp.json")
if not os.path.isfile(mcp_path) or os.path.islink(mcp_path) or os.stat(mcp_path).st_uid not in (os.getuid(), 0) or os.stat(mcp_path).st_mode & 0o022:
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
# The manifest is executed, so the resolved binary must stay inside the installed
# plugin tree -- for relative and absolute commands alike. A relative command
# resolves against the plugin (not the temp cwd), then both branches are
# realpath-resolved and containment-checked.
via_node = command == "node"
if via_node:
    if not args or not isinstance(args[0], str) or os.path.basename(args[0]) != "hackathonkit-mcp-launcher.js":
        sys.exit("[plugin-check] expected the Hackathon Agent Kit Node launcher as first argument")
    command, args = args[0], args[1:]
candidate = os.path.join(source, command)
real_command = os.path.realpath(candidate)
if not os.path.isfile(real_command) or (not via_node and not os.access(real_command, os.X_OK)):
    sys.exit("[plugin-check] launcher is not a regular executable")
if not real_command.startswith(source + os.sep):
    print("[plugin-check] refusing to execute a command outside the plugin tree: %s (resolved to %s)" % (command, real_command), file=sys.stderr)
    sys.exit(1)
component = real_command
while component != source:
    info = os.stat(component)
    if info.st_uid not in (os.getuid(), 0) or info.st_mode & 0o022:
        sys.exit("[plugin-check] unsafe writable launcher or ancestor")
    component = os.path.dirname(component)
command = real_command
cmd = [command] + [str(arg) for arg in args]
if via_node:
    node = shutil.which("node")
    if not node:
        sys.exit("[plugin-check] Node runtime not installed")
    cmd.insert(0, node)
# Apply the kit's runtime settings to this child only. A manifest must never
# inject shell startup files, loader hooks or a new executable search path.
settings = server.get("env", {})
supported = {
    "OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_TIMEOUT_SEC", "OPENAI_MODEL",
    "NVIDIA_API_KEY", "NVIDIA_BASE_URL", "POSTGRES_DSN", "MINIO_ENDPOINT",
    "MINIO_ACCESS_KEY", "MINIO_SECRET_KEY", "MINIO_BUCKET", "MINIO_USE_SSL",
    "TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "TELEGRAM_ALLOWED_CHAT_IDS",
    "TELEGRAM_API_BASE", "TELEGRAM_TIMEOUT_SEC", "HACKATHONKIT_BACKEND_TIMEOUT_SEC",
}
if not isinstance(settings, dict) or any(k not in supported or not isinstance(v, str) for k, v in settings.items()):
    sys.exit("[plugin-check] unsupported manifest env setting or value")
child_env = dict(os.environ, **settings)
with tempfile.TemporaryDirectory(prefix="hackathonkit-plugin-check-") as tmp:
    try:
        proc = subprocess.run(cmd, input=requests, text=True, errors="replace",
                              capture_output=True, cwd=tmp, timeout=120, env=child_env)
    except subprocess.TimeoutExpired:
        print("[plugin-check] FAIL: MCP command timed out after 120s", file=sys.stderr)
        sys.exit(1)
    except OSError as exc:
        print("[plugin-check] FAIL: cannot start MCP command: %s" % exc, file=sys.stderr)
        sys.exit(1)

stderr_tail = "\n".join(line for line in (proc.stderr or "").splitlines()[-5:] if line.strip())

def fail(message):
    if stderr_tail:
        message += "\n[plugin-check] server stderr:\n" + stderr_tail
    print(message, file=sys.stderr)
    sys.exit(1)

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
        fail("[plugin-check] FAIL: error response for id=%s: %s" % (obj["id"], message))

# A server that answered then died (or was signalled) is still a failure; report
# its status and stderr rather than a misleading "success".
if proc.returncode != 0:
    if proc.returncode < 0:
        fail("[plugin-check] FAIL: MCP command killed by signal %d" % -proc.returncode)
    fail("[plugin-check] FAIL: MCP command exited with status %d" % proc.returncode)

missing = [i for i in (1, 2, 3) if i not in responses]
if missing:
    fail("[plugin-check] FAIL: missing JSON-RPC responses for id(s) %s from a foreign working directory" % missing)

for want in (1, 2, 3):
    obj = responses[want]
    # 'result' may legitimately be falsy (an empty object or false), so the
    # key must exist rather than be truthy.
    if not isinstance(obj.get("result"), dict) or (want == 2 and not isinstance(obj["result"].get("tools"), list)):
        fail("[plugin-check] FAIL: response for id=%s has no result" % want)
    result = obj["result"]
    if isinstance(result, dict) and result.get("isError"):
        fail("[plugin-check] FAIL: tool call id=%s reported isError" % want)

print("[plugin-check] ok: installed plugin starts the MCP from a foreign working directory")
PY
