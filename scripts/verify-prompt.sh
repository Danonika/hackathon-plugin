#!/usr/bin/env sh
# Install/remove the Hackathon Agent Kit /verify custom prompt for Codex.
#
# Codex plugins bundle skills, MCP servers and hooks, but not slash commands;
# custom prompts live in $CODEX_HOME/prompts (default ~/.codex/prompts). The
# Hackathon Agent Kit installer places /verify there so it works like ECC's commands.
#
# Usage: verify-prompt.sh [install|remove|status]
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC="$ROOT/.codex/prompts/verify.md"
DEST_DIR="${CODEX_HOME:-$HOME/.codex}/prompts"
DEST="$DEST_DIR/verify.md"
ACTION="${1:-install}"

case "$ACTION" in
  install)
    if [ ! -f "$SRC" ]; then
      echo "[verify-prompt] source not found: $SRC" >&2
      exit 1
    fi
    mkdir -p "$DEST_DIR"
    cp "$SRC" "$DEST"
    echo "[verify-prompt] installed $DEST"
    ;;
  remove)
    if [ -f "$DEST" ] && cmp -s "$SRC" "$DEST"; then
      rm -f "$DEST"
      echo "[verify-prompt] removed $DEST"
    else
      echo "[verify-prompt] left $DEST (absent or user-modified)"
    fi
    ;;
  status)
    if [ -f "$DEST" ]; then
      echo "installed: $DEST"
    else
      echo "not installed: $DEST (run scripts/verify-prompt.sh install)"
      exit 1
    fi
    ;;
  *)
    echo "usage: verify-prompt.sh [install|remove|status]" >&2
    exit 2
    ;;
esac
