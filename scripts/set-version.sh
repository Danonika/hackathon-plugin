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

stage=""
trap '[ -z "$stage" ] || rm -f "$stage"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
replace_text() {
  stage=$(mktemp "$1.tmp.XXXXXX")
  cp -p "$1" "$stage"
  sed "$2" "$1" > "$stage"
  mv "$stage" "$1"
  stage=""
}

if [ "$#" -gt 0 ]; then
  VERSION="$1"
  if ! valid_version "$VERSION"; then
    echo "[set-version] version must be MAJOR.MINOR.PATCH (got '$VERSION')" >&2
    exit 2
  fi
  before=$(sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$MAIN" | head -n 1)
  replace_text "$MAIN" "s/^const version = \"[^\"]*\"/const version = \"$VERSION\"/"
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

# Change only the plugin version in each manifest, including the named entry
# in the marketplace. Other plugins or nested metadata can carry their own versions.
for f in "$ROOT/.claude-plugin/marketplace.json" "$ROOT/.claude-plugin/plugin.json" "$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"; do
  [ -f "$f" ] || continue
  stage=$(mktemp "$f.tmp.XXXXXX")
  cp -p "$f" "$stage"
  python3 - "$f" "$stage" "$VERSION" <<'PYTHON'
import json, os, sys
source, output, version = sys.argv[1:]
with open(source, encoding="utf-8") as stream:
    data = json.load(stream)
target = data
if os.path.basename(source) == "marketplace.json":
    entries = [item for item in data.get("plugins", []) if item.get("name") == "hackathonkit"]
    if len(entries) != 1:
        sys.exit("expected one hackathonkit plugin in " + source)
    target = entries[0]
if not isinstance(target.get("version"), str):
    sys.exit("missing plugin version in " + source)
if target["version"] != version:
    target["version"] = version
    with open(output, "w", encoding="utf-8") as stream:
        stream.write(json.dumps(data, ensure_ascii=False, indent=2) + "\n")
PYTHON
  mv "$stage" "$f"
  stage=""
done

# README status line.
if [ -f "$ROOT/README.md" ]; then
  replace_text "$ROOT/README.md" "s/^Status: v[0-9][0-9.]*\./Status: v$VERSION./"
  awk -v prefix="Status: v$VERSION." 'index($0,prefix)==1 {found=1} END {exit !found}' "$ROOT/README.md" || {
    echo "[set-version] failed to update the README status line" >&2
    exit 1
  }
fi

# HACKATHON-GUIDE duplicates the plugin version in prose.
GUIDE="$ROOT/docs/HACKATHON-GUIDE.md"
if [ -f "$GUIDE" ]; then
  if grep -q "Версия плагина: \*\*[0-9][0-9.]*\*\*" "$GUIDE"; then
    replace_text "$GUIDE" "s/Версия плагина: \*\*[0-9][0-9.]*\*\*/Версия плагина: **$VERSION**/"
  fi
  if ! grep -Fq "Версия плагина: **$VERSION**" "$GUIDE"; then
    echo "[set-version] failed to update $GUIDE (expected 'Версия плагина: **$VERSION**')" >&2
    exit 1
  fi
fi

echo "[set-version] synced manifests + docs to $VERSION"
