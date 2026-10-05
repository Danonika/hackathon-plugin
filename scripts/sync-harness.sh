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
# .claude/ has no committed files, so on a clean checkout the lock's parent does
# not exist and `mkdir "$CLAUDE_LOCK"` would fail on every attempt.
mkdir -p "$(dirname -- "$CLAUDE_LOCK")"
CLAUDE_LOCKED=0
CLAUDE_LOCK_TOKEN="$$.$(date +%s 2>/dev/null || echo 0)"
claude_lock_stale() {
  [ -d "$CLAUDE_LOCK" ] || return 1
  holder=$(cat "$CLAUDE_LOCK/pid" 2>/dev/null || true)
  now=$(date +%s 2>/dev/null || echo 0)
  mtime=$(stat -c %Y "$CLAUDE_LOCK" 2>/dev/null || stat -f %m "$CLAUDE_LOCK" 2>/dev/null || echo "$now")
  # A live pid means live. `kill -0` can also fail with EPERM for a live process
  # owned by another user, and a recycled pid can pin a dead lock, so require the
  # lock to be old before reclaiming rather than trusting a bare kill -0.
  if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
    return 1
  fi
  [ $((now - mtime)) -ge 30 ]
}
claim_claude_lock() {
  claude_lock_stale || return 0
  if mv "$CLAUDE_LOCK" "$CLAUDE_LOCK.stale.$$" 2>/dev/null; then
    rm -rf "$CLAUDE_LOCK.stale.$$"
  fi
}
claim_claude_lock
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if mkdir "$CLAUDE_LOCK" 2>/dev/null; then
    # A missing pid would make stale detection fall back to the short mtime
    # heuristic and let a slower live run lose its lock, so fail hard.
    if ! printf '%s' "$$" > "$CLAUDE_LOCK/pid" 2>/dev/null; then
      echo "[sync-harness] cannot record the lock pid; aborting" >&2
      rm -rf "$CLAUDE_LOCK"
      exit 1
    fi
    printf '%s' "$CLAUDE_LOCK_TOKEN" > "$CLAUDE_LOCK/owner" 2>/dev/null || true
    CLAUDE_LOCKED=1
    break
  fi
  claim_claude_lock
  sleep 1
done
if [ "$CLAUDE_LOCKED" != "1" ]; then
  echo "[sync-harness] another run holds $CLAUDE_LOCK; aborting" >&2
  exit 1
fi
# Reconcile staging/backup directories orphaned by a SIGKILL between the two
# rename halves of a previous swap. We hold the lock, so any such directory
# belongs to a dead run and can be removed; the next run's $$ suffix would
# otherwise never match them, leaking them forever.
for stale in \
  "$ROOT/.claude/skills.old."* "$ROOT/.claude/skills.tmp."* \
  "$ROOT/.opencode/commands.old."* "$ROOT/.opencode/commands.tmp."* \
  "$ROOT/.claude/commands.old."* "$ROOT/.claude/commands.tmp."*; do
  [ -e "$stale" ] || [ -L "$stale" ] || continue
  rm -rf "$stale"
done
CLAUDE_TMP="$CLAUDE_DST.tmp.$$"
CLAUDE_OLD=""
CMDS_STAGE=""
CMDS_OLD=""
CMDS_DEST=""
release_claude_lock() {
  [ "$CLAUDE_LOCKED" = "1" ] || return 0
  owner=$(cat "$CLAUDE_LOCK/owner" 2>/dev/null || true)
  pid_owner=$(cat "$CLAUDE_LOCK/pid" 2>/dev/null || true)
  if [ "$owner" = "$CLAUDE_LOCK_TOKEN" ] || { [ -z "$owner" ] && [ "$pid_owner" = "$$" ]; }; then
    if mv "$CLAUDE_LOCK" "$CLAUDE_LOCK.done.$$" 2>/dev/null; then
      rm -rf "$CLAUDE_LOCK.done.$$"
    fi
  fi
  CLAUDE_LOCKED=0
}

finish_harness() {
  rm -rf "$CLAUDE_TMP"
  # Restore an interrupted command-mirror swap before discarding the staging dir.
  if [ -n "$CMDS_OLD" ] && [ -n "$CMDS_DEST" ] && [ -d "$CMDS_OLD" ] && [ ! -d "$CMDS_DEST" ]; then
    if mv "$CMDS_OLD" "$CMDS_DEST" 2>/dev/null; then
      CMDS_OLD=""
    else
      echo "[sync-harness] WARNING: could not restore $CMDS_DEST from $CMDS_OLD; leaving the backup in place" >&2
    fi
  fi
  if [ -n "$CMDS_OLD" ] && [ -n "$CMDS_DEST" ] && [ -d "$CMDS_DEST" ]; then
    rm -rf "$CMDS_OLD"
  fi
  if [ -n "$CMDS_STAGE" ]; then rm -rf "$CMDS_STAGE"; fi
  if [ -n "$CLAUDE_OLD" ] && [ -d "$CLAUDE_OLD" ] && [ ! -d "$CLAUDE_DST" ]; then
    if mv "$CLAUDE_OLD" "$CLAUDE_DST" 2>/dev/null; then
      CLAUDE_OLD=""
    else
      echo "[sync-harness] WARNING: could not restore $CLAUDE_DST from $CLAUDE_OLD; leaving the backup in place" >&2
    fi
  fi
  # Only delete the backup once the live mirror is known to exist, so a failed
  # restore never destroys the only good copy.
  if [ -n "$CLAUDE_OLD" ] && [ -d "$CLAUDE_DST" ]; then
    rm -rf "$CLAUDE_OLD"
  fi
  release_claude_lock
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
  CMDS_DEST="$dest"
  CMDS_OLD=""
  rm -rf "$CMDS_STAGE"
  mkdir -p "$CMDS_STAGE"
  for f in "$CMDS_SRC"/*.md; do
    [ -f "$f" ] || continue
    cp "$f" "$CMDS_STAGE/$(basename -- "$f")"
  done
  CMDS_OLD="$dest.old.$$"
  if [ -d "$dest" ]; then
    if ! mv "$dest" "$CMDS_OLD"; then
      echo "[sync-harness] cannot move aside $dest" >&2
      exit 1
    fi
  fi
  if ! mv "$CMDS_STAGE" "$dest"; then
    echo "[sync-harness] command mirror swap failed for $dest" >&2
    if [ -d "$CMDS_OLD" ]; then mv "$CMDS_OLD" "$dest"; fi
    exit 1
  fi
  rm -rf "$CMDS_OLD"
  CMDS_STAGE=""
  CMDS_OLD=""
  CMDS_DEST=""
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
    first=$(head -c 1 "$1" 2>/dev/null || true)
    last=$(tail -c 2 "$1" 2>/dev/null | tr -d '\n' || true)
    [ "$first" = "{" ] && [ "${last%\}}" != "$last" ]
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
