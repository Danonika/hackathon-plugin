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

. "$ROOT/scripts/skill-profile.sh"
PROFILE="${1:-$(installed_skill_profile "$ROOT")}"
if ! sh "$ROOT/scripts/sync-skills.sh" "$PROFILE"; then
  echo "[sync-harness] sync-skills.sh failed" >&2
  exit 1
fi

# Claude Code reads project skills from .claude/skills. Mirror atomically so a
# failed copy cannot leave the mirror missing or partial.
CLAUDE_DST="$ROOT/.claude/skills"
mkdir -p "$ROOT/data/locks" "$ROOT/.claude"
. "$ROOT/scripts/file-lock.sh"
acquire_file_lock "$ROOT/data/locks/claude-skills.lock"
# Restore a surviving prior swap before sweeping dead-run staging directories.
for live in "$CLAUDE_DST" "$ROOT/.opencode/commands" "$ROOT/.claude/commands"; do
  for previous in "$live.old."*; do
    [ -d "$previous" ] || continue
    if [ ! -e "$live" ] && [ ! -L "$live" ]; then mv "$previous" "$live"
    else rm -rf "$previous"; fi
  done
  for stale in "$live.tmp."*; do
    [ -e "$stale" ] || continue
    rm -rf "$stale"
  done
done
CLAUDE_TMP="$CLAUDE_DST.tmp.$$"
CMDS_STAGE=""
finish_harness() {
  rm -rf "$CLAUDE_TMP"
  if [ -n "$CMDS_STAGE" ]; then rm -rf "$CMDS_STAGE"; fi
}
trap finish_harness EXIT
# Signal handlers only exit; the EXIT trap owns cleanup (calling finish_harness
# here would run it twice).
trap 'exit 130' INT
trap 'exit 143' TERM
if [ ! -d "$ROOT/plugins/hackathonkit/skills" ]; then
  echo "[sync-harness] skills source not found: $ROOT/plugins/hackathonkit/skills" >&2
  exit 1
fi
rm -rf "$CLAUDE_TMP"
mkdir -p "$CLAUDE_TMP"
cp -RP "$ROOT/plugins/hackathonkit/skills/." "$CLAUDE_TMP/"
. "$ROOT/scripts/publish-mirror.sh"
filter_skill_stage "$CLAUDE_TMP" "$PROFILE"
publish_mirror "$CLAUDE_TMP" "$CLAUDE_DST"

# Mirror the plugin commands into the harness project command directories:
# OpenCode reads .opencode/commands/<name>.md and Claude Code reads
# .claude/commands/. The plugin directory is the single source of truth.
# Stage each mirror and swap it in while still holding the lock, so concurrent
# runs cannot expose partially written files and removed commands are pruned.
CMDS_SRC="$ROOT/plugins/hackathonkit/commands"
if [ ! -d "$CMDS_SRC" ]; then
  echo "[sync-harness] commands source not found: $CMDS_SRC" >&2
  exit 1
fi
for dest in "$ROOT/.opencode/commands" "$ROOT/.claude/commands"; do
  mkdir -p "$(dirname -- "$dest")"
  CMDS_STAGE="$dest.tmp.$$"
  rm -rf "$CMDS_STAGE"
  mkdir -p "$CMDS_STAGE"
  for f in "$CMDS_SRC"/*.md; do
    [ -f "$f" ] || continue
    cp "$f" "$CMDS_STAGE/$(basename -- "$f")"
  done
  publish_mirror "$CMDS_STAGE" "$dest"
  rm -rf "$CMDS_STAGE"
  CMDS_STAGE=""
done
# Keep the lock held through the adapter/mirror verification below so a
# concurrent run cannot swap or prune the command mirrors mid-check. The EXIT
# trap releases it (and drops the traps) at the very end of the script.

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
plugins/hackathonkit/commands/plan.md
plugins/hackathonkit/commands/research.md
plugins/hackathonkit/commands/build.md
plugins/hackathonkit/commands/evaluate.md
plugins/hackathonkit/commands/verify.md"
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
# Probe the comparison tool first: without this, an absent `cmp` would fail
# every comparison and misreport all mirrors as missing or stale.
if command -v cmp >/dev/null 2>&1; then
  files_equal() { cmp -s "$1" "$2"; }
elif command -v diff >/dev/null 2>&1; then
  files_equal() { diff -q "$1" "$2" >/dev/null 2>&1; }
else
  echo "[sync-harness] neither cmp nor diff is available; cannot verify command mirrors" >&2
  exit 1
fi
# Verify the command mirrors just written match their canonical source, so a
# silent failure of the mirroring loop cannot be reported as "adapters ready".
for dest in "$ROOT/.opencode/commands" "$ROOT/.claude/commands"; do
  for f in "$CMDS_SRC"/*.md; do
    [ -f "$f" ] || continue
    name=$(basename -- "$f")
    if [ ! -f "$dest/$name" ] || ! files_equal "$f" "$dest/$name"; then
      echo "[sync-harness] command mirror missing or stale: $dest/$name" >&2
      MISSING=$((MISSING + 1))
    fi
  done
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
    awk '{ gsub(/[[:space:]]/,""); if (length($0)) { if (!first) first=substr($0,1,1); last=substr($0,length($0)) } } END { exit !(first=="{" && last=="}") }' "$1"
  fi
}

for f in $JSON_MANIFESTS; do
  # A missing manifest is already counted by the ADAPTERS loop above; skip it
  # here so it is reported once (invalid) rather than twice.
  [ -f "$ROOT/$f" ] || continue
  if ! json_valid "$ROOT/$f"; then
    echo "[sync-harness] invalid JSON manifest: $f" >&2
    INVALID=$((INVALID + 1))
  fi
done
if [ "$MISSING" -ne 0 ] || [ "$INVALID" -ne 0 ]; then
  echo "[sync-harness] $MISSING adapter/mirror file(s) missing or stale, $INVALID manifest(s) invalid" >&2
  exit 1
fi

COUNT=$(find "$ROOT/.claude/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
CMDS_COUNT=$(find "$ROOT/.opencode/commands" -mindepth 1 -maxdepth 1 -type f -name '*.md' 2>/dev/null | wc -l | tr -d ' ')
echo "[sync-harness] adapters ready: codex, opencode, claude, cursor ($COUNT skills mirrored to .claude/skills, $CMDS_COUNT command(s) mirrored)"
