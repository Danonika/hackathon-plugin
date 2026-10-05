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
TEMPLATE="$ROOT/plugins/hackathonkit/.mcp.json.template"

if [ ! -d "$ROOT" ]; then
  echo "[mcp-manifest] repository root not found: '$ROOT'" >&2
  exit 1
fi
# Canonicalise to an absolute physical path so a relative/`..`/symlinked caller
# value cannot be written into the manifest as a relative path.
ROOT="$(CDPATH= cd -- "$ROOT" 2>/dev/null && pwd -P)" || {
  echo "[mcp-manifest] cannot resolve repository root: '${1:-}'" >&2
  exit 1
}
if [ ! -f "$TEMPLATE" ]; then
  echo "[mcp-manifest] template not found: $TEMPLATE" >&2
  exit 1
fi

json_escape() {
  # JSON escaper for path-like values: backslash and quote are escaped and every
  # control character is dropped (such bytes cannot appear in a path and would
  # otherwise produce invalid JSON).
  printf '%s' "$1" | LC_ALL=C tr -d '[:cntrl:]' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

MANIFEST="$ROOT/plugins/hackathonkit/.mcp.json"
TMP=$(mktemp "$ROOT/plugins/hackathonkit/.mcp.json.tmp.XXXXXX")
cleanup_manifest() { rm -f "$TMP"; }
trap cleanup_manifest EXIT
trap 'cleanup_manifest; exit 130' INT
trap 'cleanup_manifest; exit 143' TERM

ROOT_JSON="$(json_escape "$ROOT")" LAUNCHER_JSON="$(json_escape "hackathonkit-mcp-launcher.sh")" awk '
  BEGIN { root = ENVIRON["ROOT_JSON"]; launcher = ENVIRON["LAUNCHER_JSON"] }
  {
    pos = 1
    while ((i = index(substr($0, pos), "{{REPO_ROOT}}")) > 0) {
      start = pos + i - 1
      $0 = substr($0, 1, start - 1) root substr($0, start + 13)
      pos = start + length(root)
    }
    pos = 1
    while ((i = index(substr($0, pos), "{{MCP_LAUNCHER}}")) > 0) {
      start = pos + i - 1
      $0 = substr($0, 1, start - 1) launcher substr($0, start + 16)
      pos = start + length(launcher)
    }
    print
  }
' "$TEMPLATE" > "$TMP"

if grep -q -e "{{REPO_ROOT}}" -e "{{MCP_LAUNCHER}}" "$TMP"; then
  echo "[mcp-manifest] unexpanded placeholder remains; keeping the previous manifest" >&2
  exit 1
fi
if command -v python3 >/dev/null 2>&1; then
  if ! python3 -c 'import json,sys; json.load(open(sys.argv[1]))' "$TMP" 2>/dev/null; then
    echo "[mcp-manifest] rendered manifest is not valid JSON; keeping the previous manifest" >&2
    exit 1
  fi
else
  # Without a validator a malformed manifest could be published and break every
  # MCP client; make the reduced guarantee visible.
  echo "[mcp-manifest] warning: python3 unavailable; cannot validate the rendered manifest" >&2
fi

chmod 0644 "$TMP"
mv "$TMP" "$MANIFEST"
echo "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $ROOT"
