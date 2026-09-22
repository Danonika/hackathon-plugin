#!/usr/bin/env sh
# Generate harness mirrors and verify committed adapter manifests.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)" || {
  echo "[sync-harness] cannot resolve the repository root" >&2
  exit 1
}
if [ ! -f "$ROOT/docker-compose.yml" ]; then
  echo "[sync-harness] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi

if ! sh "$ROOT/scripts/sync-skills.sh"; then
  echo "[sync-harness] sync-skills.sh failed" >&2
  exit 1
fi

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
CLAUDE_DST="$ROOT/.claude/skills"
CLAUDE_LOCK="$CLAUDE_DST.lock"
CLAUDE_LOCKED=0
if [ -d "$CLAUDE_LOCK" ]; then
  HOLDER=$(cat "$CLAUDE_LOCK/pid" 2>/dev/null || true)
  if [ -n "$HOLDER" ] && ! kill -0 "$HOLDER" 2>/dev/null; then
    rm -rf "$CLAUDE_LOCK"
  fi
fi
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$CLAUDE_LOCK" 2>/dev/null; then
    printf '%s' "$$" > "$CLAUDE_LOCK/pid" 2>/dev/null || true
    CLAUDE_LOCKED=1
    break
  fi
  sleep 1
done
if [ "$CLAUDE_LOCKED" != "1" ]; then
  echo "[sync-harness] another run holds $CLAUDE_LOCK; aborting" >&2
  exit 1
fi
CLAUDE_TMP="$CLAUDE_DST.tmp.$$"
CLAUDE_OLD=""
finish_harness() {
  rm -rf "$CLAUDE_TMP"
  if [ -n "$CLAUDE_OLD" ] && [ -d "$CLAUDE_OLD" ] && [ ! -d "$CLAUDE_DST" ]; then
    if ! mv "$CLAUDE_OLD" "$CLAUDE_DST" 2>/dev/null; then
      echo "[sync-harness] WARNING: could not restore $CLAUDE_DST from $CLAUDE_OLD" >&2
    fi
  fi
  rm -rf "$CLAUDE_OLD"
  if [ "$CLAUDE_LOCKED" = "1" ]; then
    rm -rf "$CLAUDE_LOCK"
  fi
}
trap finish_harness EXIT
trap 'finish_harness; exit 130' INT
trap 'finish_harness; exit 143' TERM
if [ ! -d "$ROOT/plugins/hackathonkit/skills" ]; then
  echo "[sync-harness] skills source not found: $ROOT/plugins/hackathonkit/skills" >&2
  exit 1
fi
rm -rf "$CLAUDE_TMP"
mkdir -p "$CLAUDE_TMP"
cp -R "$ROOT/plugins/hackathonkit/skills/." "$CLAUDE_TMP/"
CLAUDE_OLD="$CLAUDE_DST.old.$$"
if [ -d "$CLAUDE_DST" ]; then
  mv "$CLAUDE_DST" "$CLAUDE_OLD"
fi
if ! mv "$CLAUDE_TMP" "$CLAUDE_DST"; then
  echo "[sync-harness] mirror swap failed; restoring the previous mirror" >&2
  [ -d "$CLAUDE_OLD" ] && mv "$CLAUDE_OLD" "$CLAUDE_DST"
  exit 1
fi
rm -rf "$CLAUDE_OLD"
CLAUDE_LOCKED=0
rm -rf "$CLAUDE_LOCK"
trap - EXIT INT TERM

# Mirror the plugin commands into the harness project command directories:
# OpenCode reads .opencode/commands/<name>.md and Claude Code reads
# .claude/commands/. The plugin directory is the single source of truth.
CMDS_SRC="$ROOT/plugins/hackathonkit/commands"
if [ ! -d "$CMDS_SRC" ]; then
  echo "[sync-harness] commands source not found: $CMDS_SRC" >&2
  exit 1
fi
for dest in "$ROOT/.opencode/commands" "$ROOT/.claude/commands"; do
  mkdir -p "$dest"
  for f in "$CMDS_SRC"/*.md; do
    [ -f "$f" ] || continue
    cp "$f" "$dest/$(basename -- "$f")"
  done
done

MISSING=0
INVALID=0
ADAPTERS="AGENTS.md
.codex/AGENTS.md
.codex/config.toml
.codex/agents/researcher.toml
.codex/agents/reviewer.toml
.codex/agents/evaluator.toml
.claude-plugin/plugin.json
.claude-plugin/marketplace.json
.opencode/opencode.json
.cursor/rules/hackathonkit.mdc
plugins/hackathonkit/hooks/codex-hooks.json
plugins/hackathonkit/commands/verify.md
.codex/prompts/verify.md"
JSON_MANIFESTS=".claude-plugin/plugin.json
.claude-plugin/marketplace.json
.opencode/opencode.json
plugins/hackathonkit/hooks/codex-hooks.json"
for f in $ADAPTERS; do
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
    # No validator available: a shallow check only. Warn so this is not
    # mistaken for real JSON validation.
    echo "[sync-harness] warning: no JSON validator (python3/node) found; using a shallow check" >&2
    first=$(head -c 1 "$1" 2>/dev/null || true)
    last=$(tail -c 2 "$1" 2>/dev/null | tr -d '\n' || true)
    [ "$first" = "{" ] && [ "${last%\}}" != "$last" ]
  fi
}

for f in $JSON_MANIFESTS; do
  if ! json_valid "$ROOT/$f"; then
    echo "[sync-harness] invalid JSON manifest: $f" >&2
    INVALID=$((INVALID + 1))
  fi
done
if [ "$MISSING" -ne 0 ] || [ "$INVALID" -ne 0 ]; then
  echo "[sync-harness] $MISSING adapter file(s) missing, $INVALID manifest(s) invalid" >&2
  exit 1
fi

COUNT=$(find "$ROOT/.claude/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
CMDS_COUNT=$(find "$ROOT/.opencode/commands" -mindepth 1 -maxdepth 1 -type f -name '*.md' 2>/dev/null | wc -l | tr -d ' ')
echo "[sync-harness] adapters ready: codex, opencode, claude, cursor ($COUNT skills mirrored to .claude/skills, $CMDS_COUNT command(s) mirrored)"
