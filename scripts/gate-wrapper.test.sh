#!/usr/bin/env sh
# Exercise the gate and skill-validation entrypoints in an isolated checkout.
set -eu
scripts=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
repo="$work/repo"
mkdir -p "$repo/scripts" "$repo/bin" "$repo/plugins/hackathonkit/skills" "$repo/docs" "$work/tools"
cp "$scripts/quality-gate.sh" "$scripts/validate-skills.sh" "$repo/scripts/"
touch "$repo/go.mod" "$repo/docs/SKILL-STANDARD.md"
cat > "$repo/bin/hackathonkit-mcp" <<'STUB'
#!/bin/sh
echo installed >> "$CHECK_LOG"
exit "${CHECK_STATUS:-0}"
STUB
cat > "$work/tools/go" <<'STUB'
#!/bin/sh
if [ "$1" = run ]; then echo 'go run' >> "$CHECK_LOG"; exit "${CHECK_STATUS:-0}"; fi
while [ "$#" -gt 0 ] && [ "$1" != -o ]; do shift; done
shift
cp "$TEST_RUNTIME" "$1"
chmod +x "$1"
echo build >> "$CHECK_LOG"
STUB
chmod +x "$repo/bin/hackathonkit-mcp" "$work/tools/go"
tool_path=$work/tools
for name in sh dirname id find mktemp rm cat cp chmod head sed; do
  ln -s "$(command -v "$name")" "$tool_path/$name"
done
export CHECK_LOG="$work/checks" TEST_RUNTIME="$repo/bin/hackathonkit-mcp"
run_check() {
  expected=$1
  shift
  status=0
  PATH="$tool_path" "$@" >"$work/out" 2>&1 || status=$?
  if [ "$status" != "$expected" ]; then
    cat "$work/out" >&2
    echo "expected $expected, got $status for $*" >&2
    exit 1
  fi
}
touch -t 203001010000 "$TEST_RUNTIME"
run_check 0 sh "$repo/scripts/quality-gate.sh"
[ "$(cat "$CHECK_LOG")" = "$(printf 'build\ninstalled')" ]
: > "$CHECK_LOG"
export HACKATHONKIT_QUALITY_FROM_SOURCE=1 CHECK_STATUS=7
run_check 7 sh "$repo/scripts/quality-gate.sh"
[ "$(cat "$CHECK_LOG")" = "$(printf 'build\ninstalled')" ]
unset HACKATHONKIT_QUALITY_FROM_SOURCE CHECK_STATUS
run_check 0 sh "$repo/scripts/validate-skills.sh"
export CHECK_STATUS=9
run_check 9 sh "$repo/scripts/validate-skills.sh"
mv "$tool_path/go" "$work/go.save"
run_check 9 sh "$repo/scripts/quality-gate.sh"
mv "$work/go.save" "$tool_path/go"
rm -f "$repo/bin/hackathonkit-mcp"
run_check 9 sh "$repo/scripts/validate-skills.sh"
rm -f "$tool_path/go"
run_check 2 sh "$repo/scripts/validate-skills.sh"
run_check 2 sh "$repo/scripts/quality-gate.sh"
echo "[gate-wrapper-test] gate and validator entrypoints passed"
