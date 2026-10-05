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
if [ -z "${CODEX_HOME:-}" ] && [ -z "${HOME:-}" ]; then
  echo "[prompts] neither CODEX_HOME nor HOME is set; cannot determine the prompts directory" >&2
  exit 1
fi
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
    skipped=0
    for f in "$SRC_DIR"/*.md; do
      [ -f "$f" ] || continue
      dest="$DEST_DIR/$(basename -- "$f")"
      # Any existing non-regular destination (directory, symlink, FIFO, socket,
      # device) is refused: cp would write into it, follow it, or block on it.
      if [ -L "$dest" ] || { [ -e "$dest" ] && [ ! -f "$dest" ]; }; then
        echo "[prompts] skipping $dest (not a regular file)" >&2
        skipped=$((skipped + 1))
        continue
      fi
      if [ -f "$dest" ] && ! cmp -s "$f" "$dest"; then
        if cp -p "$dest" "$dest.bak" 2>/dev/null; then
          echo "[prompts] warning: backing up locally modified prompt $dest to $dest.bak before overwriting" >&2
        else
          echo "[prompts] warning: overwriting locally modified prompt $dest (backup failed)" >&2
        fi
      fi
      cp "$f" "$dest"
      n=$((n + 1))
    done
    echo "[prompts] installed $n prompt(s) to $DEST_DIR"
    if [ "$skipped" -gt 0 ]; then
      echo "[prompts] skipped $skipped non-regular destination(s)" >&2
    fi
    ;;
  remove)
    removed=0
    if [ -d "$DEST_DIR" ]; then
      for f in "$SRC_DIR"/*.md; do
        [ -f "$f" ] || continue
        dest="$DEST_DIR/$(basename -- "$f")"
        # `[ -f ]` follows symlinks; require a non-symlink regular file so a
        # user-created link is never deleted, matching the install guard.
        if [ -f "$dest" ] && [ ! -L "$dest" ] && cmp -s "$f" "$dest"; then
          rm -f "$dest"
          removed=$((removed + 1))
        fi
      done
    fi
    echo "[prompts] removed $removed unmodified prompt(s) from $DEST_DIR"
    ;;
  status)
    missing=0
    stale=0
    for f in "$SRC_DIR"/*.md; do
      [ -f "$f" ] || continue
      dest="$DEST_DIR/$(basename -- "$f")"
      # Count non-regular destinations (symlinks, directories, ...) as missing:
      # install would skip them, so reporting them installed would be a lie.
      if [ -L "$dest" ] || [ ! -f "$dest" ]; then
        missing=$((missing + 1))
      elif ! cmp -s "$f" "$dest"; then
        stale=$((stale + 1))
      fi
    done
    if [ "$missing" -eq 0 ] && [ "$stale" -eq 0 ]; then
      echo "installed: $DEST_DIR"
    else
      echo "not up to date ($missing missing, $stale out of date): $DEST_DIR (run scripts/install-prompts.sh install)"
      exit 1
    fi
    ;;
  *)
    echo "usage: install-prompts.sh [install|remove|status]" >&2
    exit 2
    ;;
esac
