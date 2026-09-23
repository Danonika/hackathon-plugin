#!/usr/bin/env sh
# Regression tests for scripts/set-version.sh: validation, verification, idempotency.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
HELPER="$ROOT/scripts/set-version.sh"
FAIL=0

check() {
  name="$1"; ok="$2"; detail="$3"
  if [ "$ok" = "1" ]; then echo "[ok]   $name"
  else echo "[fail] $name${detail:+: $detail}"; FAIL=$((FAIL + 1)); fi
}

version_of() { sed -n 's/^const version = "\([^"]*\)".*/\1/p' "$1" | head -n 1; }

# A scratch copy of the repository metadata, with a minimal main.go.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM
mkdir -p "$WORK/cmd/hackathonkit-mcp" "$WORK/.claude-plugin" "$WORK/plugins/hackathonkit/.codex-plugin" "$WORK/docs" "$WORK/scripts"
printf 'package main\n\nconst version = "1.2.3"\n\nfunc main() {}\n' > "$WORK/cmd/hackathonkit-mcp/main.go"
printf '{\n  "version": "1.2.3"\n}\n' > "$WORK/.claude-plugin/plugin.json"
printf '{\n  "version": "1.2.3"\n}\n' > "$WORK/.claude-plugin/marketplace.json"
printf '{\n  "version": "1.2.3"\n}\n' > "$WORK/plugins/hackathonkit/.codex-plugin/plugin.json"
printf 'Status: v1.2.3. body\n' > "$WORK/README.md"
printf 'Версия плагина: **1.2.3** · rest\n' > "$WORK/docs/HACKATHON-GUIDE.md"
cp "$HELPER" "$WORK/scripts/set-version.sh"

# Invalid version must be rejected (exit 2), leaving files untouched.
if sh "$WORK/scripts/set-version.sh" "not-a-version" >/dev/null 2>&1; then
  check "invalid version rejected" 0 "expected non-zero exit"
else
  check "invalid version rejected" 1
fi
if sh "$WORK/scripts/set-version.sh" "1.2.x" >/dev/null 2>&1; then
  check "non-numeric version rejected" 0 "expected non-zero exit"
else
  check "non-numeric version rejected" 1
fi

# Valid bump updates every target.
sh "$WORK/scripts/set-version.sh" "2.0.0" >/dev/null 2>&1
check "main.go bumped" "$([ "$(version_of "$WORK/cmd/hackathonkit-mcp/main.go")" = "2.0.0" ] && echo 1)" "got $(version_of "$WORK/cmd/hackathonkit-mcp/main.go")"
check "plugin.json bumped" "$(grep -q '"version": "2.0.0"' "$WORK/.claude-plugin/plugin.json" && echo 1)"
check "marketplace bumped" "$(grep -q '"version": "2.0.0"' "$WORK/.claude-plugin/marketplace.json" && echo 1)"
check "README bumped" "$(grep -q '^Status: v2.0.0\.' "$WORK/README.md" && echo 1)"
check "guide bumped" "$(grep -q 'Версия плагина: \*\*2.0.0\*\*' "$WORK/docs/HACKATHON-GUIDE.md" && echo 1)"

# Idempotent: running again with the same version changes nothing.
before=$(cat "$WORK/.claude-plugin/plugin.json")
sh "$WORK/scripts/set-version.sh" >/dev/null 2>&1
check "idempotent" "$([ "$before" = "$(cat "$WORK/.claude-plugin/plugin.json")" ] && echo 1)"

# A drifted manifest (pattern no longer matches) must fail loudly.
printf '{ "version" : "9.9.9" }\n' > "$WORK/.claude-plugin/plugin.json"
if sh "$WORK/scripts/set-version.sh" >/dev/null 2>&1; then
  check "drifted manifest fails" 0 "expected non-zero exit"
else
  check "drifted manifest fails" 1
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[set-version-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[set-version-test] all assertions passed"
