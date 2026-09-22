#!/usr/bin/env sh
# Install/remove the Hackathon Agent Kit command prompts for Codex.
#
# Codex plugins bundle skills, MCP servers and hooks, but not slash commands;
# custom prompts live in $CODEX_HOME/prompts (default ~/.codex/prompts). The
# source of truth is plugins/hackathonkit/commands/*.md.
#
# Usage: install-prompts.sh [install|remove|status]
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
SRC_DIR="$ROOT/plugins/hackathonkit/commands"
DEST_DIR="${CODEX_HOME:-$HOME/.codex}/prompts"
ACTION="${1:-install}"

if [ ! -d "$SRC_DIR" ]; then
  echo "[prompts] source not found: $SRC_DIR" >&2
  exit 1
fi

case "$ACTION" in
  install)
    mkdir -p "$DEST_DIR"
    n=0
    for f in "$SRC_DIR"/*.md; do
      [ -f "$f" ] || continue
      cp "$f" "$DEST_DIR/$(basename -- "$f")"
      n=$((n + 1))
    done
    echo "[prompts] installed $n prompt(s) to $DEST_DIR"
    ;;
  remove)
    for f in "$SRC_DIR"/*.md; do
      [ -f "$f" ] || continue
      dest="$DEST_DIR/$(basename -- "$f")"
      if [ -f "$dest" ] && cmp -s "$f" "$dest"; then
        rm -f "$dest"
      fi
    done
    echo "[prompts] removed unmodified prompts from $DEST_DIR"
    ;;
  status)
    missing=0
    for f in "$SRC_DIR"/*.md; do
      [ -f "$f" ] || continue
      [ -f "$DEST_DIR/$(basename -- "$f")" ] || missing=$((missing + 1))
    done
    if [ "$missing" -eq 0 ]; then
      echo "installed: $DEST_DIR"
    else
      echo "not installed ($missing missing): $DEST_DIR (run scripts/install-prompts.sh install)"
      exit 1
    fi
    ;;
  *)
    echo "usage: install-prompts.sh [install|remove|status]" >&2
    exit 2
    ;;
esac
