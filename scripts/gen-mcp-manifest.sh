#!/usr/bin/env sh
# Hydrate plugins/hackathonkit/.mcp.json from the template with an absolute root.
set -e
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
TEMPLATE="$ROOT/plugins/hackathonkit/.mcp.json.template"

if [ -z "$ROOT" ] || [ ! -d "$ROOT" ]; then
  echo "[mcp-manifest] repository root not found: '$ROOT'" >&2
  exit 1
fi
if [ ! -f "$TEMPLATE" ]; then
  echo "[mcp-manifest] template not found: $TEMPLATE" >&2
  exit 1
fi

json_escape() {
  printf '%s' "$1" | tr -d '\r\n' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e 's/	/\\t/g'
}

TMP="$ROOT/plugins/hackathonkit/.mcp.json.tmp.$$"
trap 'rm -f "$TMP"' EXIT INT TERM

ROOT_JSON="$(json_escape "$ROOT")" LAUNCHER_JSON="$(json_escape "hackathonkit-mcp-launcher.sh")" awk '
  BEGIN { root = ENVIRON["ROOT_JSON"]; launcher = ENVIRON["LAUNCHER_JSON"] }
  {
    while ((i = index($0, "{{REPO_ROOT}}")) > 0) {
      $0 = substr($0, 1, i - 1) root substr($0, i + 13)
    }
    while ((i = index($0, "{{MCP_LAUNCHER}}")) > 0) {
      $0 = substr($0, 1, i - 1) launcher substr($0, i + 16)
    }
    print
  }
' "$TEMPLATE" > "$TMP"

if grep -q "{{" "$TMP"; then
  echo "[mcp-manifest] unexpanded placeholder remains; keeping the previous manifest" >&2
  exit 1
fi

mv "$TMP" "$ROOT/plugins/hackathonkit/.mcp.json"
echo "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $ROOT"
