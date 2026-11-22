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
printf '%s\n' '{"plugins":[{"name":"hackathonkit","version":"1.2.3"},{"name":"other","version":"9.0.0"}]}' > "$WORK/.claude-plugin/marketplace.json"
printf '{\n  "version": "1.2.3"\n}\n' > "$WORK/plugins/hackathonkit/.codex-plugin/plugin.json"
printf 'Status: v1.2.3. body\n' > "$WORK/README.md"
printf 'Версия плагина: **1.2.3** · rest\n' > "$WORK/docs/HACKATHON-GUIDE.md"
cp "$HELPER" "$WORK/scripts/set-version.sh"

# Invalid version must be rejected (exit 2), leaving files untouched.
metadata() {
  (cd "$WORK" && find . -type f ! -path './scripts/*' -exec cksum {} \; | LC_ALL=C sort)
}
before=$(metadata)
for bad in not-a-version 1.2.x 1.2 1.2.3.4 ""; do
  status=0
  sh "$WORK/scripts/set-version.sh" "$bad" >/dev/null 2>&1 || status=$?
  check "reject invalid version '$bad' with exit 2" "$([ "$status" = 2 ] && echo 1)"
done
check "invalid input keeps all metadata" "$([ "$before" = "$(metadata)" ] && echo 1)"

# A valid bump: tolerate a non-zero exit so an unexpected failure is reported by
# the assertions below rather than aborting the suite under `set -e`.
if sh "$WORK/scripts/set-version.sh" "2.0.0" >/dev/null 2>&1; then :; else
  check "set-version.sh 2.0.0 exited 0" 0 "non-zero exit"
fi
check "main.go bumped" "$([ "$(version_of "$WORK/cmd/hackathonkit-mcp/main.go")" = "2.0.0" ] && echo 1)" "got $(version_of "$WORK/cmd/hackathonkit-mcp/main.go")"
check "plugin.json bumped" "$(grep -q '"version": "2.0.0"' "$WORK/.claude-plugin/plugin.json" && echo 1)"
check "marketplace bumped" "$(grep -q '"version": "2.0.0"' "$WORK/.claude-plugin/marketplace.json" && echo 1)"
check "README bumped" "$(grep -q '^Status: v2.0.0\.' "$WORK/README.md" && echo 1)"
check "guide bumped" "$(grep -q 'Версия плагина: \*\*2.0.0\*\*' "$WORK/docs/HACKATHON-GUIDE.md" && echo 1)"

check "Codex manifest bumped" "$(grep -Fq '"version": "2.0.0"' "$WORK/plugins/hackathonkit/.codex-plugin/plugin.json" && echo 1)"
check "other marketplace version preserved" "$(grep -Fq '"version": "9.0.0"' "$WORK/.claude-plugin/marketplace.json" && echo 1)"

# Idempotent: running again with the same version changes nothing.
before=$(metadata)
if sh "$WORK/scripts/set-version.sh" >/dev/null 2>&1; then :; else
  check "set-version.sh re-run exited 0" 0 "non-zero exit"
fi
check "idempotent" "$([ "$before" = "$(metadata)" ] && echo 1)"

# The guide file being absent must not fail the sync.
mv "$WORK/docs/HACKATHON-GUIDE.md" "$WORK/docs/HACKATHON-GUIDE.md.bak"
if sh "$WORK/scripts/set-version.sh" "2.0.1" >/dev/null 2>&1; then
  check "absent guide tolerated" 1
else
  check "absent guide tolerated" 0 "non-zero exit"
fi
mv "$WORK/docs/HACKATHON-GUIDE.md.bak" "$WORK/docs/HACKATHON-GUIDE.md"

# A drifted manifest (pattern no longer matches) must fail loudly.
printf '{ "nested": {"version":"9.9.9"} }\n' > "$WORK/.claude-plugin/plugin.json"
if sh "$WORK/scripts/set-version.sh" >"$WORK/drift.log" 2>&1; then
  check "drifted manifest fails" 0 "expected non-zero exit"
else
  status=$?
  check "drifted manifest fails" "$([ "$status" = 1 ] && grep -Fq "missing plugin version" "$WORK/drift.log" && echo 1)"
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[set-version-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[set-version-test] all assertions passed"
