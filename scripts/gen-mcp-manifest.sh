#!/usr/bin/env sh
# Hydrate plugins/hackathonkit/.mcp.json from the template with an absolute root.
set -e
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
TEMPLATE="$ROOT/plugins/hackathonkit/.mcp.json.template"

if [ ! -f "$TEMPLATE" ]; then
  echo "[mcp-manifest] template not found: $TEMPLATE" >&2
  exit 1
fi

TMP="$ROOT/plugins/hackathonkit/.mcp.json.tmp.$$"
trap 'rm -f "$TMP"' EXIT INT TERM

awk -v root="$ROOT" '{
  while ((i = index($0, "{{REPO_ROOT}}")) > 0) {
    $0 = substr($0, 1, i - 1) root substr($0, i + 13)
  }
  print
}' "$TEMPLATE" > "$TMP"

mv "$TMP" "$ROOT/plugins/hackathonkit/.mcp.json"
echo "[mcp-manifest] wrote plugins/hackathonkit/.mcp.json for $ROOT"
