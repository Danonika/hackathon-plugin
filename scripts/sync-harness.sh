#!/usr/bin/env sh
# Generate harness mirrors and verify committed adapter manifests.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)"

sh "$ROOT/scripts/sync-skills.sh"

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
CLAUDE_DST="$ROOT/.claude/skills"
CLAUDE_TMP="$CLAUDE_DST.tmp.$$"
cleanup_tmp() { rm -rf "$CLAUDE_TMP"; }
on_signal() { cleanup_tmp; exit 130; }
trap cleanup_tmp EXIT
trap on_signal INT TERM
rm -rf "$CLAUDE_TMP"
mkdir -p "$CLAUDE_TMP"
cp -R "$ROOT/plugins/hackathonkit/skills/." "$CLAUDE_TMP/"
CLAUDE_OLD="$CLAUDE_DST.old.$$"
if [ -d "$CLAUDE_DST" ]; then
  mv "$CLAUDE_DST" "$CLAUDE_OLD"
fi
if ! mv "$CLAUDE_TMP" "$CLAUDE_DST"; then
  [ -d "$CLAUDE_OLD" ] && mv "$CLAUDE_OLD" "$CLAUDE_DST"
  exit 1
fi
rm -rf "$CLAUDE_OLD"
trap - EXIT INT TERM

MISSING=0
for f in \
  AGENTS.md \
  .codex/AGENTS.md \
  .codex/config.toml \
  .codex/agents/researcher.toml \
  .codex/agents/reviewer.toml \
  .codex/agents/evaluator.toml \
  .claude-plugin/plugin.json \
  .claude-plugin/marketplace.json \
  .opencode/opencode.json \
  .cursor/rules/hackathonkit.mdc \
  plugins/hackathonkit/hooks/codex-hooks.json
do
  if [ ! -f "$ROOT/$f" ]; then
    echo "[sync-harness] missing adapter file: $f" >&2
    MISSING=$((MISSING + 1))
  fi
done
json_valid() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json, sys; json.load(open(sys.argv[1]))' "$1" >/dev/null 2>&1
  elif command -v node >/dev/null 2>&1; then
    node -e 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8"))' "$1" >/dev/null 2>&1
  else
    # No validator available: at least require a plausible object start.
    head -c 1 "$1" 2>/dev/null | grep -q '{'
  fi
}

for f in .claude-plugin/plugin.json .claude-plugin/marketplace.json .opencode/opencode.json plugins/hackathonkit/hooks/codex-hooks.json; do
  if ! json_valid "$ROOT/$f"; then
    echo "[sync-harness] invalid JSON manifest: $f" >&2
    MISSING=$((MISSING + 1))
  fi
done
if [ "$MISSING" -ne 0 ]; then
  echo "[sync-harness] $MISSING adapter file(s) missing or invalid" >&2
  exit 1
fi

COUNT=$(find "$ROOT/.claude/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
echo "[sync-harness] adapters ready: codex, opencode, claude, cursor ($COUNT skills mirrored to .claude/skills)"
