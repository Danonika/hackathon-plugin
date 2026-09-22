#!/usr/bin/env sh
# Regression tests for the /verify command: canonical source, harness
# registration and generated mirrors.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
CMD="$ROOT/plugins/hackathonkit/commands/verify.md"
TMPHOME="$(mktemp -d)"
cleanup_vc_test() { rm -rf "$TMPHOME"; }
trap cleanup_vc_test 0
trap 'cleanup_vc_test; exit 130' INT
trap 'cleanup_vc_test; exit 143' TERM
FAIL=0

check() {
  name="$1"
  ok="$2"
  detail="$3"
  if [ "$ok" = "1" ]; then
    echo "[ok]   $name"
  else
    echo "[fail] $name${detail:+: $detail}"
    FAIL=$((FAIL + 1))
  fi
}

# Canonical command exists and describes the gate + coverage.
if [ -f "$CMD" ]; then
  check "canonical verify command exists" 1
else
  check "canonical verify command exists" 0 "missing $CMD"
fi
check "command runs the quality gate" "$(grep -q 'quality-gate.sh' "$CMD" 2>/dev/null && echo 1 || echo 0)"
check "command enforces coverage" "$(grep -q 'coverage' "$CMD" 2>/dev/null && echo 1 || echo 0)"
check "command documents --target" "$(grep -q -- '--target' "$CMD" 2>/dev/null && echo 1 || echo 0)"
check "command reports seven checks" "$(grep -q 'seven checks' "$CMD" 2>/dev/null && echo 1 || echo 0)"

# Claude Code plugin manifest declares the command.
if command -v python3 >/dev/null 2>&1; then
  check "claude plugin declares the command" "$(python3 - "$ROOT/.claude-plugin/plugin.json" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except Exception:
    print(0); raise SystemExit
cmds = data.get("commands") or []
ok = any("commands/verify.md" in str(c) for c in cmds)
print(1 if ok else 0)
PY
)" "$(cat "$ROOT/.claude-plugin/plugin.json" 2>/dev/null | tr -d '\n' | head -c 120)"
else
  check "claude plugin declares the command" "$(grep -q 'commands/verify.md' "$ROOT/.claude-plugin/plugin.json" 2>/dev/null && echo 1 || echo 0)"
fi

# Codex prompt ships and mentions coverage.
CODEX_PROMPT="$ROOT/.codex/prompts/verify.md"
check "codex prompt exists" "$([ -f "$CODEX_PROMPT" ] && echo 1 || echo 0)" "missing $CODEX_PROMPT"
check "codex prompt mentions coverage" "$(grep -q 'coverage' "$CODEX_PROMPT" 2>/dev/null && echo 1 || echo 0)"

# sync-harness mirrors the command into the harness project command dirs.
sh "$ROOT/scripts/sync-harness.sh" >/dev/null 2>&1 || true
for mirror in "$ROOT/.opencode/commands/verify.md" "$ROOT/.claude/commands/verify.md"; do
  rel=${mirror#"$ROOT/"}
  if [ -f "$mirror" ] && cmp -s "$CMD" "$mirror"; then
    check "mirror $rel matches the canonical file" 1
  else
    check "mirror $rel matches the canonical file" 0 "missing or stale; run scripts/sync-harness.sh"
  fi
done

# The Codex prompt installer places /verify in $CODEX_HOME/prompts.
check "setup installs the Codex prompt" "$(grep -q 'verify-prompt.sh' "$ROOT/scripts/setup.sh" 2>/dev/null && echo 1 || echo 0)"
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/verify-prompt.sh" install >/dev/null 2>&1
check "prompt installer writes the Codex prompt" "$([ -f "$TMPHOME/prompts/verify.md" ] && echo 1 || echo 0)"
if CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/verify-prompt.sh" status >/dev/null 2>&1; then
  check "prompt installer reports installed" 1
else
  check "prompt installer reports installed" 0 "status failed after install"
fi
printf '\ncustom edit\n' >> "$TMPHOME/prompts/verify.md"
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/verify-prompt.sh" remove >/dev/null 2>&1
check "prompt installer preserves a user-modified prompt" "$([ -f "$TMPHOME/prompts/verify.md" ] && echo 1 || echo 0)"
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/verify-prompt.sh" install >/dev/null 2>&1
CODEX_HOME="$TMPHOME" sh "$ROOT/scripts/verify-prompt.sh" remove >/dev/null 2>&1
check "prompt installer removes an unmodified prompt" "$([ -f "$TMPHOME/prompts/verify.md" ] && echo 0 || echo 1)"

if [ "$FAIL" -ne 0 ]; then
  echo "[verify-command-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[verify-command-test] all assertions passed"
