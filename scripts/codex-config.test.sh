#!/usr/bin/env sh
# Regression tests for .codex/config.toml placement and the config generator.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FAIL=0
check() {
  name="$1"; ok="$2"; detail="$3"
  if [ "$ok" = "1" ]; then echo "[ok]   $name"
  else echo "[fail] $name${detail:+: $detail}"; FAIL=$((FAIL + 1)); fi
}

CONFIG="$ROOT/.codex/config.toml"

# Root-level keys must be parsed as top-level, not as server options (TOML table
# scoping: a key after [mcp_servers.hackathonkit] would be swallowed by that table).
if command -v python3 >/dev/null 2>&1; then
  TOP=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
server = d.get("mcp_servers", {}).get("hackathonkit", {})
# web_search must be a root key, not a server key.
print("ok" if "web_search" in d and "web_search" not in server else "bad")
' "$CONFIG")
  check "web_search is a root-level key" "$([ "$TOP" = "ok" ] && echo 1)" "got: $TOP"
fi

# The generator rewrites args to an absolute launcher path that exists.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
mkdir -p "$WORK/.codex" "$WORK/plugins/hackathonkit/scripts"
cp "$CONFIG" "$WORK/.codex/config.toml"
: > "$WORK/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1 || true
if command -v python3 >/dev/null 2>&1; then
  ARGS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
print(d["mcp_servers"]["hackathonkit"]["args"][0])
' "$WORK/.codex/config.toml")
  check "generator writes an absolute launcher path" "$(printf '%s' "$ARGS" | grep -q "^/" && echo 1)" "got: $ARGS"
  check "generated launcher path exists" "$([ -f "$ARGS" ] && echo 1)" "missing: $ARGS"
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[codex-config-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[codex-config-test] all assertions passed"
