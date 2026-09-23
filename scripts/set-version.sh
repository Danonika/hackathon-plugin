#!/usr/bin/env sh
# Set one version everywhere it is duplicated (source of truth: cmd/hackathonkit-mcp).
#
# Usage:
#   scripts/set-version.sh              # sync all manifests/README to the Go const
#   scripts/set-version.sh 0.17.0       # set the Go const too, then sync
#
# Regenerated state (configs/install-state.json) is intentionally left alone:
# scripts/write-install-state.sh owns it.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
MAIN="$ROOT/cmd/hackathonkit-mcp/main.go"

if [ ! -f "$MAIN" ]; then
  echo "[set-version] main.go not found: $MAIN" >&2
  exit 1
fi

if [ -n "$1" ]; then
  VERSION="$1"
  case "$VERSION" in
    [0-9]*.[0-9]*.[0-9]*) : ;;
    *) echo "[set-version] version must look like MAJOR.MINOR.PATCH (got '$VERSION')" >&2; exit 2 ;;
  esac
  sed -i.bak "s/^const version = \"[^\"]*\"/const version = \"$VERSION\"/" "$MAIN"
  rm -f "$MAIN.bak"
  echo "[set-version] main.go -> $VERSION"
fi

VERSION=$(sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$MAIN" | head -n 1)
if [ -z "$VERSION" ]; then
  echo "[set-version] could not read the version from main.go" >&2
  exit 1
fi

# JSON manifests: replace the first "version": "..." value in each.
for f in "$ROOT/.claude-plugin/marketplace.json" "$ROOT/.claude-plugin/plugin.json" "$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"; do
  [ -f "$f" ] || continue
  sed -i.bak "s/\"version\": \"[0-9][0-9.]*\"/\"version\": \"$VERSION\"/" "$f"
  rm -f "$f.bak"
done
# README status line.
if [ -f "$ROOT/README.md" ]; then
  sed -i.bak "s/^Status: v[0-9][0-9.]*\./Status: v$VERSION./" "$ROOT/README.md"
  rm -f "$ROOT/README.md.bak"
fi
echo "[set-version] synced manifests + README to $VERSION"
