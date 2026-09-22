#!/usr/bin/env sh
# Remove generated Hackathon Agent Kit artifacts. Runtime state (data/control) is removed;
# data/input, data/artifacts, logs and committed source stay in place.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)" || {
  echo "[uninstall] cannot resolve the repository root" >&2
  exit 1
}
if [ -z "$ROOT" ] || [ "$ROOT" = "/" ] || [ ! -f "$ROOT/docker-compose.yml" ] || [ ! -f "$ROOT/go.mod" ] || [ ! -d "$ROOT/plugins/hackathonkit/skills" ]; then
  echo "[uninstall] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi

APPLY=0
for arg in "$@"; do
  case "$arg" in
    --yes) APPLY=1 ;;
    -h|--help)
      echo "usage: scripts/uninstall.sh [--yes]   (without --yes this is a dry run)"
      exit 0
      ;;
    *)
      echo "[uninstall] unknown argument: $arg" >&2
      exit 1
      ;;
  esac
done

# Shared skill mirrors: remove only the skills this plugin ships so
# user-authored skills survive.
for rel in .agents/skills .claude/skills; do
  mirror="$ROOT/$rel"
  [ -d "$mirror" ] || continue
  for skill in "$ROOT/plugins/hackathonkit/skills"/*/; do
    [ -d "$skill" ] || continue
    name=$(basename "$skill")
    if [ "$APPLY" = "1" ]; then
      rm -rf "$mirror/$name" 2>/dev/null || FAILURES=$((FAILURES + 1))
    else
      echo "would remove: $rel/$name"
    fi
  done
done

CODEX_FAIL=0
if ! command -v codex >/dev/null 2>&1; then
  echo "[uninstall] codex CLI not found; skipping plugin/marketplace removal"
fi
if command -v codex >/dev/null 2>&1; then
  if [ "$APPLY" = "1" ]; then
    if ! codex plugin remove hackathonkit >/dev/null 2>&1; then
      echo "[uninstall] warning: codex plugin remove failed" >&2
      CODEX_FAIL=1
    fi
    if ! codex plugin marketplace remove hackathonkit-local >/dev/null 2>&1; then
      echo "[uninstall] warning: codex marketplace remove failed" >&2
      CODEX_FAIL=1
    fi
    if [ "$CODEX_FAIL" -eq 0 ]; then
      echo "[uninstall] removed hackathonkit plugin and marketplace"
    else
      echo "[uninstall] plugin/marketplace removal reported warnings" >&2
    fi
  else
    echo "would run: codex plugin remove hackathonkit"
    echo "would run: codex plugin marketplace remove hackathonkit-local"
  fi
fi

# Remove the /verify custom prompt this installer placed for Codex.
if [ "$APPLY" = "1" ]; then
  sh "$ROOT/scripts/verify-prompt.sh" remove >/dev/null 2>&1 || true
else
  echo "would remove: $HOME/.codex/prompts/verify.md (if unmodified)"
fi

FAILURES=0
for rel in \
  plugins/hackathonkit/.mcp.json \
  configs/install-state.json \
  data/control \
  bin
do
  if [ "$APPLY" = "1" ]; then
    if ! rm -rf "$ROOT/$rel" 2>/dev/null; then
      echo "[uninstall] warning: could not remove $rel" >&2
      FAILURES=$((FAILURES + 1))
    fi
  else
    echo "would remove: $rel"
  fi
done

if [ "$APPLY" != "1" ]; then
  echo "[uninstall] dry run: re-run with --yes to apply"
  exit 0
fi

if [ "$FAILURES" -gt 0 ] || [ "$CODEX_FAIL" -ne 0 ]; then
  echo "[uninstall] finished with warnings ($FAILURES path failure(s), codex failures: $CODEX_FAIL)" >&2
  exit 1
fi
echo "[uninstall] done. data/input, data/artifacts, logs/ and committed files were kept (data/control was removed)."
