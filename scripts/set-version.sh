#!/usr/bin/env sh
# Set one version everywhere it is duplicated (source of truth: cmd/hackathonkit-mcp).
#
# Usage:
#   scripts/set-version.sh              # sync manifests/docs to the Go const
#   scripts/set-version.sh 0.17.0       # set the Go const too, then sync
#
# Every edit is verified: a target whose pattern no longer matches fails loudly
# instead of silently keeping the old value. Regenerated state
# (configs/install-state.json) is intentionally left alone:
# scripts/write-install-state.sh owns it.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
MAIN="$ROOT/cmd/hackathonkit-mcp/main.go"

if [ ! -f "$MAIN" ]; then
  echo "[set-version] main.go not found: $MAIN" >&2
  exit 1
fi

# Validate MAJOR.MINOR.PATCH with a strict regex (POSIX case globs cannot
# anchor `.` or repeat groups precisely).
valid_version() {
  # grep matches any line, so an embedded newline (e.g. "1.2.3\ne touch /tmp/x")
  # would pass the pattern and then inject a sed command via the replacement.
  # Reject newlines before matching so only a single clean version is accepted.
  case "$1" in
    *'
'*) return 1 ;;
  esac
  printf '%s' "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'
}

if [ -n "$1" ]; then
  VERSION="$1"
  if ! valid_version "$VERSION"; then
    echo "[set-version] version must be MAJOR.MINOR.PATCH (got '$VERSION')" >&2
    exit 2
  fi
  before=$(sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$MAIN" | head -n 1)
  sed -i.bak "s/^const version = \"[^\"]*\"/const version = \"$VERSION\"/" "$MAIN"
  rm -f "$MAIN.bak"
  after=$(sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$MAIN" | head -n 1)
  if [ "$after" != "$VERSION" ]; then
    echo "[set-version] failed to update main.go (was '$before', expected '$VERSION')" >&2
    exit 1
  fi
  echo "[set-version] main.go -> $VERSION"
fi

VERSION=$(sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$MAIN" | head -n 1)
if ! valid_version "$VERSION"; then
  echo "[set-version] could not read a valid version from main.go" >&2
  exit 1
fi

# JSON manifests: replace the version value in each, then verify.
for f in "$ROOT/.claude-plugin/marketplace.json" "$ROOT/.claude-plugin/plugin.json" "$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"; do
  [ -f "$f" ] || continue
  sed -i.bak "s/\"version\": *\"[0-9][0-9.]*\"/\"version\": \"$VERSION\"/" "$f"
  rm -f "$f.bak"
  if ! grep -Eq "\"version\": *\"$VERSION\"" "$f"; then
    echo "[set-version] failed to update $f" >&2
    exit 1
  fi
done

# README status line.
if [ -f "$ROOT/README.md" ]; then
  sed -i.bak "s/^Status: v[0-9][0-9.]*\./Status: v$VERSION./" "$ROOT/README.md"
  rm -f "$ROOT/README.md.bak"
  grep -Eq "^Status: v$VERSION\." "$ROOT/README.md" || {
    echo "[set-version] failed to update the README status line" >&2
    exit 1
  }
fi

# HACKATHON-GUIDE duplicates the plugin version in prose.
GUIDE="$ROOT/docs/HACKATHON-GUIDE.md"
if [ -f "$GUIDE" ]; then
  if grep -q "Версия плагина: \*\*[0-9][0-9.]*\*\*" "$GUIDE"; then
    sed -i.bak "s/Версия плагина: \*\*[0-9][0-9.]*\*\*/Версия плагина: **$VERSION**/" "$GUIDE"
    rm -f "$GUIDE.bak"
  fi
  if ! grep -Fq "Версия плагина: **$VERSION**" "$GUIDE"; then
    echo "[set-version] failed to update $GUIDE (expected 'Версия плагина: **$VERSION**')" >&2
    exit 1
  fi
fi

echo "[set-version] synced manifests + docs to $VERSION"
