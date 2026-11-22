#!/usr/bin/env sh
# Hydrate plugins/hackathonkit/.mcp.json from the template with an absolute root.
set -e
# Resolve the script path so a slashless `$0` (invoked via PATH) still finds the
# repository instead of `.` = the current working directory.
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)}"

if [ ! -d "$ROOT" ]; then
  echo "[mcp-manifest] repository root not found: '$ROOT'" >&2
  exit 1
fi
# Canonicalise to an absolute physical path so a relative/`..`/symlinked caller
# value cannot be written into the manifest as a relative path.
REQUESTED_ROOT="$ROOT"
ROOT="$(CDPATH= cd -- "$ROOT" 2>/dev/null && pwd -P)" || {
  # Report the value that actually failed, not just an empty caller argument:
  # the default (derived) root is exactly the interesting case.
  echo "[mcp-manifest] cannot resolve repository root: '$REQUESTED_ROOT'" >&2
  exit 1
}
TEMPLATE="$ROOT/plugins/hackathonkit/.mcp.json.template"
if [ ! -f "$TEMPLATE" ]; then
  echo "[mcp-manifest] template not found: $TEMPLATE" >&2
  exit 1
fi

json_escape() {
  # JSON escaper for path-like values: backslash and quote are escaped. Control
  # characters are rejected rather than deleted: a path may legally contain C0
  # bytes, and dropping them would publish a manifest pointing at a different
  # path (a silent corruption) while a raw control byte is invalid JSON.
  case "$1" in
    *[[:cntrl:]]*)
      echo "[mcp-manifest] path contains control characters; refusing to write it" >&2
      return 1
      ;;
  esac
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

. "$(dirname -- "$0")/private-write.sh"
plugin="$ROOT/plugins/hackathonkit"
for path in "$ROOT/plugins" "$plugin" "$plugin/scripts" "$plugin/scripts/hackathonkit-mcp-launcher.sh"; do
  private_write "$path"
done
MANIFEST="$plugin/.mcp.json"
TMP=$(mktemp "$ROOT/plugins/hackathonkit/.mcp.json.tmp.XXXXXX")
cleanup_manifest() { rm -f "$TMP"; }
trap cleanup_manifest EXIT
trap 'cleanup_manifest; exit 130' INT
trap 'cleanup_manifest; exit 143' TERM

# Capture the escapes separately so a rejected (control-char) value fails the
# script under `set -e` instead of being silently swallowed by the awk command.
ROOT_JSON=$(json_escape "$ROOT") || exit 1
LAUNCHER_JSON=$(json_escape "hackathonkit-mcp-launcher.sh") || exit 1
export ROOT_JSON LAUNCHER_JSON
awk '
  {
    line = $0
    while (match(line, /\{\{[^{}]+\}\}/)) {
      printf "%s", substr(line, 1, RSTART-1)
      key = substr(line, RSTART, RLENGTH)
      if (key == "{{REPO_ROOT}}") printf "%s", ENVIRON["ROOT_JSON"]
      else if (key == "{{MCP_LAUNCHER}}") printf "%s", ENVIRON["LAUNCHER_JSON"]
      else { print "unknown manifest placeholder: " key > "/dev/stderr"; exit 1 }
      line = substr(line, RSTART + RLENGTH)
    }
    print line
  }
' "$TEMPLATE" > "$TMP"

if command -v python3 >/dev/null 2>&1; then
  python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$TMP"
elif command -v node >/dev/null 2>&1; then
  node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$TMP"
elif command -v jq >/dev/null 2>&1; then
  jq -e 'type == "object"' "$TMP" >/dev/null
else
  echo "[mcp-manifest] Python, Node or jq is required to validate the manifest" >&2
  exit 1
fi

chmod 0644 "$TMP"
mv "$TMP" "$MANIFEST"
echo "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $ROOT"
