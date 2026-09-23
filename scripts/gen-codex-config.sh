#!/usr/bin/env sh
# Hydrate .codex/config.toml with an absolute launcher path. The committed file
# uses a placeholder-free relative command so it works from the repo root; this
# script rewrites the [mcp_servers.hackathonkit] command to the absolute path, which
# works from any working directory and with paths containing spaces.
#
# Usage: scripts/gen-codex-config.sh [repo-root]
set -e
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
CONFIG="$ROOT/.codex/config.toml"

if [ -z "$ROOT" ] || [ ! -d "$ROOT" ]; then
  echo "[codex-config] repository root not found: '$ROOT'" >&2
  exit 1
fi
ROOT="$(CDPATH= cd -- "$ROOT" && pwd)"
if [ ! -f "$CONFIG" ]; then
  echo "[codex-config] config not found: $CONFIG" >&2
  exit 1
fi

# TOML basic-string escape for a path: backslash and quote only (paths do not
# contain control characters).
toml_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

LAUNCHER="$ROOT/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
if [ ! -f "$LAUNCHER" ]; then
  echo "[codex-config] launcher not found: $LAUNCHER" >&2
  exit 1
fi

TMP=$(mktemp "$ROOT/.codex/config.toml.tmp.XXXXXX")
cleanup() { rm -f "$TMP"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
sed -e "s#^command = .*#command = \"sh\"#" \
    -e "s#^args = .*#args = [\"$(toml_escape "$LAUNCHER")\"]#" \
    "$CONFIG" > "$TMP"

if command -v python3 >/dev/null 2>&1; then
  if ! python3 -c 'import tomllib,sys; tomllib.load(open(sys.argv[1],"rb"))' "$TMP" 2>/dev/null; then
    echo "[codex-config] rendered config is not valid TOML; keeping the previous file" >&2
    exit 1
  fi
fi

chmod 0644 "$TMP"
mv "$TMP" "$CONFIG"
echo "[codex-config] pinned [mcp_servers.hackathonkit] to $LAUNCHER"
