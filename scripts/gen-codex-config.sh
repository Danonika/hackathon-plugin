#!/usr/bin/env sh
# Pin the Hackathon Agent Kit MCP launcher to an absolute path in the USER-level Codex
# config ($CODEX_HOME/config.toml, default ~/.codex/config.toml).
#
# The committed project file (.codex/config.toml) stays machine-neutral and
# cwd-relative; this writes the machine-specific override to the user config,
# which Codex applies with higher precedence. Never rewrites a tracked file.
#
# Usage: scripts/gen-codex-config.sh [repo-root]
set -e
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"

if [ -z "$ROOT" ] || [ ! -d "$ROOT" ]; then
  echo "[codex-config] repository root not found: '$ROOT'" >&2
  exit 1
fi
ROOT="$(CDPATH= cd -- "$ROOT" && pwd)"
LAUNCHER="$ROOT/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
if [ ! -f "$LAUNCHER" ]; then
  echo "[codex-config] launcher not found: $LAUNCHER" >&2
  exit 1
fi

CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
DEST="$CODEX_DIR/config.toml"
BEGIN="# >>> hackathonkit-mcp (managed by scripts/gen-codex-config.sh) <<<"
END="# <<< hackathonkit-mcp (managed by scripts/gen-codex-config.sh) >>>"

toml_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

mkdir -p "$CODEX_DIR"
TMP=$(mktemp "$CODEX_DIR/config.toml.tmp.XXXXXX")
cleanup() { rm -f "$TMP"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

# Preserve user content while replacing our managed block (if present).
if [ -f "$DEST" ]; then
  awk -v begin="$BEGIN" -v end="$END" '
    $0 == begin { inside = 1; next }
    $0 == end { inside = 0; next }
    !inside { print }
  ' "$DEST" > "$TMP"
else
  : > "$TMP"
fi

{
  printf '\n%s\n' "$BEGIN"
  printf '[mcp_servers.hackathonkit]\n'
  printf 'command = "sh"\n'
  printf 'args = ["%s"]\n' "$(toml_escape "$LAUNCHER")"
  printf '%s\n' "$END"
} >> "$TMP"

if command -v python3 >/dev/null 2>&1; then
  if ! python3 -c 'import tomllib,sys; tomllib.load(open(sys.argv[1],"rb"))' "$TMP" 2>/dev/null; then
    echo "[codex-config] rendered config is not valid TOML; keeping the previous file" >&2
    exit 1
  fi
fi

chmod 0644 "$TMP"
mv "$TMP" "$DEST"
echo "[codex-config] pinned hackathonkit MCP to $LAUNCHER in $DEST"
