#!/usr/bin/env sh
# Remove generated Hackathon Agent Kit artifacts. Runtime state (data/control) is removed;
# data/input, data/artifacts, logs and committed source stay in place.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *) SELF=$(command -v -- "$SELF" 2>/dev/null || echo "$SELF") ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname -- "$SELF")/.." && pwd)"
if [ -z "$ROOT" ] || [ "$ROOT" = "/" ] || [ ! -f "$ROOT/docker-compose.yml" ] || [ ! -f "$ROOT/go.mod" ]; then
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

CODEX_FAIL=0
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
    echo "[uninstall] removed hackathonkit plugin and marketplace"
  else
    echo "would run: codex plugin remove hackathonkit"
    echo "would run: codex plugin marketplace remove hackathonkit-local"
  fi
fi

FAILURES=0
for rel in \
  plugins/hackathonkit/.mcp.json \
  .agents/skills \
  .claude/skills \
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
  echo "[uninstall] finished with warnings ($FAILURES path failure(s))" >&2
  exit 1
fi
echo "[uninstall] done. data/, logs/ and committed files were kept."
