#!/usr/bin/env sh
# Regression tests for the plugin commands: canonical sources, harness
# registration, generated mirrors and the Codex prompt installer.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
CMDS_DIR="$ROOT/plugins/hackathonkit/commands"
COMMANDS="plan research build evaluate verify"
TMPHOME="$(mktemp -d)"
cleanup_vc_test() { rm -rf "$TMPHOME"; }
trap cleanup_vc_test 0
trap 'cleanup_vc_test; exit 130' INT
trap 'cleanup_vc_test; exit 143' TERM
FAIL=0

check() {
  if [ "$2" = "1" ]; then
    echo "[ok]   $1"
  else
    echo "[fail] $1${3:+: $3}"
    FAIL=$((FAIL + 1))
  fi
}

# Every command has a canonical markdown source.
for cmd in $COMMANDS; do
  if [ -f "$CMDS_DIR/$cmd.md" ]; then
    check "canonical /$cmd command exists" 1
  else
    check "canonical /$cmd command exists" 0 "missing $CMDS_DIR/$cmd.md"
  fi
done

# The commands tell the agent to run the work itself (not the user). Assert per
# file: `grep -q pattern f1 f2 f3` would pass if only one file matched.
for cmd in plan research build; do
  check "$cmd states the agent runs the tools" "$(grep -q 'do not ask the user' "$CMDS_DIR/$cmd.md" 2>/dev/null && echo 1 || echo 0)" "missing instruction in $cmd.md"
done
check "evaluate assigns execution to the agent" "$(grep -q 'The agent runs the evaluation tools' "$CMDS_DIR/evaluate.md" && echo 1)"
check "verify runs the quality gate" "$(grep -q 'quality-gate.sh' "$CMDS_DIR/verify.md" 2>/dev/null && echo 1 || echo 0)"
check "verify enforces coverage" "$(grep -q 'coverage' "$CMDS_DIR/verify.md" 2>/dev/null && echo 1 || echo 0)"
check "verify documents --target" "$(grep -q -- '--target' "$CMDS_DIR/verify.md" 2>/dev/null && echo 1 || echo 0)"
check "verify reports seven checks" "$(grep -q 'seven checks' "$CMDS_DIR/verify.md" 2>/dev/null && echo 1 || echo 0)"
# /build runs the code-review pair in order: cp-expert-refactoring before system-design-expert.
build_md="$CMDS_DIR/build.md"
if awk '/^[0-9]+[.]/ { pair=($0 ~ /code review pair/) } pair { text=text " " $0 } END { a=index(text,"cp-expert-refactoring"); b=index(text,"system-design-expert"); exit !(a>0 && b>a) }' "$build_md"; then
  check "build requires the ordered code review pair" 1
else
  check "build requires the ordered code review pair" 0
fi

# The Claude Code plugin manifest declares every command.
if command -v python3 >/dev/null 2>&1; then
  check "claude plugin declares all commands" "$(python3 - "$ROOT/.claude-plugin/plugin.json" $COMMANDS <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print(0); raise SystemExit
cmds = data.get("commands") or []
want = sys.argv[2:]
ok = all(any(f"commands/{w}.md" in str(c) for c in cmds) for w in want)
print(1 if ok else 0)
PY
)"
else
  declared=1
  for cmd in $COMMANDS; do
    grep -q "commands/$cmd.md" "$ROOT/.claude-plugin/plugin.json" 2>/dev/null || declared=0
  done
  check "claude plugin declares all commands" "$declared"
fi

# sync-harness mirrors every command into the harness project command dirs.
sh "$ROOT/scripts/sync-harness.sh" >/dev/null 2>&1 || check "harness sync succeeded" 0
for cmd in $COMMANDS; do
  for mpath in "$ROOT/.opencode/commands/$cmd.md" "$ROOT/.claude/commands/$cmd.md"; do
    rel=${mpath#"$ROOT/"}
    if [ -f "$mpath" ] && cmp -s "$CMDS_DIR/$cmd.md" "$mpath"; then
      check "mirror $rel matches canonical" 1
    else
      check "mirror $rel matches canonical" 0 "missing or stale; run scripts/sync-harness.sh"
    fi
  done
done

# The Codex prompt installer places every command in $CODEX_HOME/prompts.
check "setup installs the Codex prompts" "$(grep -q 'install-prompts.sh' "$ROOT/scripts/setup.sh" 2>/dev/null && echo 1 || echo 0)"
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/install-prompts.sh" install >/dev/null 2>&1 || check "prompt installer succeeded" 0
missing=0
for cmd in $COMMANDS; do
  [ -f "$TMPHOME/prompts/$cmd.md" ] || missing=$((missing + 1))
done
check "prompt installer writes every command" "$([ "$missing" -eq 0 ] && echo 1 || echo 0)" "$missing missing"
if CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/install-prompts.sh" status >/dev/null 2>&1; then
  check "prompt installer reports installed" 1
else
  check "prompt installer reports installed" 0 "status failed after install"
fi
printf '\ncustom edit\n' >> "$TMPHOME/prompts/verify.md"
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/install-prompts.sh" remove >/dev/null 2>&1 || check "prompt removal succeeded" 0
# Contract: `install-prompts.sh remove` deletes only prompts whose installed
# content still matches the shipped source; a prompt the user modified is kept.
# These assertions pin that behavior, so an installer change (e.g. removing the
# whole prompts directory, or switching to checksums) must update them too.
check "prompt installer preserves a user-modified prompt" "$([ -f "$TMPHOME/prompts/verify.md" ] && grep -q 'custom edit' "$TMPHOME/prompts/verify.md" && echo 1 || echo 0)"
check "prompt installer removes unmodified prompts" "$([ -f "$TMPHOME/prompts/plan.md" ] && echo 0 || echo 1)"

if [ "$FAIL" -ne 0 ]; then
  echo "[verify-command-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[verify-command-test] all assertions passed"
