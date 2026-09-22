#!/usr/bin/env sh
# Remove generated Hackathon Agent Kit artifacts. Runtime state (data/control) is removed;
# data/input, data/artifacts, logs and committed source stay in place.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
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

if command -v codex >/dev/null 2>&1; then
  if [ "$APPLY" = "1" ]; then
    codex plugin remove hackathonkit >/dev/null 2>&1 || true
    codex plugin marketplace remove hackathonkit-local >/dev/null 2>&1 || true
    echo "[uninstall] removed hackathonkit plugin and marketplace"
  else
    echo "would run: codex plugin remove hackathonkit"
    echo "would run: codex plugin marketplace remove hackathonkit-local"
  fi
fi

for rel in \
  plugins/hackathonkit/.mcp.json \
  .agents/skills \
  .claude/skills \
  configs/install-state.json \
  data/control \
  bin
do
  if [ "$APPLY" = "1" ]; then
    rm -rf "$ROOT/$rel"
  else
    echo "would remove: $rel"
  fi
done

if [ "$APPLY" != "1" ]; then
  echo "[uninstall] dry run: re-run with --yes to apply"
  exit 0
fi

echo "[uninstall] done. data/, logs/ and committed files were kept."
