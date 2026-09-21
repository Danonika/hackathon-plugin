#!/usr/bin/env sh
# Generate harness mirrors and verify committed adapter manifests.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

sh "$ROOT/scripts/sync-skills.sh"

# Claude Code reads project skills from .claude/skills.
rm -rf "$ROOT/.claude/skills"
mkdir -p "$ROOT/.claude/skills"
cp -R "$ROOT/plugins/hackathonkit/skills/." "$ROOT/.claude/skills/"

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
if [ "$MISSING" -ne 0 ]; then
  echo "[sync-harness] $MISSING adapter file(s) missing" >&2
  exit 1
fi

COUNT=$(find "$ROOT/.claude/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
echo "[sync-harness] adapters ready: codex, opencode, claude, cursor ($COUNT skills mirrored to .claude/skills)"
