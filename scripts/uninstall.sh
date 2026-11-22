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
FAILURES=0
for rel in .agents/skills .claude/skills; do
  mirror="$ROOT/$rel"
  [ -d "$mirror" ] || continue
  for skill in "$ROOT/plugins/hackathonkit/skills"/*/; do
    [ -d "$skill" ] || continue
    name=$(basename "$skill")
    if [ "$APPLY" = "1" ]; then
      # Only remove a mirror entry that is still an unmodified copy of the
      # shipped skill; a user-authored skill with the same name is preserved.
      if [ -d "$mirror/$name" ]; then
        if diff -r "$skill" "$mirror/$name" >/dev/null 2>&1; then
          rm -rf "$mirror/$name" 2>/dev/null || { echo "[uninstall] could not remove $rel/$name" >&2; FAILURES=$((FAILURES + 1)); }
        else
          echo "[uninstall] keeping modified $rel/$name (differs from the shipped skill)" >&2
        fi
      fi
    else
      # Mirror the apply-path conditions so the preview does not advertise a
      # removal that would actually be skipped (modified user copies are kept).
      if [ -d "$mirror/$name" ]; then
        if diff -r "$skill" "$mirror/$name" >/dev/null 2>&1; then
          echo "would remove: $rel/$name"
        else
          echo "[uninstall] would keep modified $rel/$name (differs from the shipped skill)" >&2
        fi
      fi
    fi
  done
done

registered() {
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import json,sys
try:
    rows = json.load(sys.stdin)[sys.argv[1]]
    if not isinstance(rows, list): raise ValueError()
    sys.exit(0 if any(isinstance(row,dict) and row.get("name")==sys.argv[2] for row in rows) else 1)
except (ValueError, TypeError, KeyError):
    sys.exit(2)' "$1" "$2"
  elif command -v node >/dev/null 2>&1; then
    node -e 'try {
      const rows = JSON.parse(require("fs").readFileSync(0,"utf8"))[process.argv[1]];
      process.exit(rows.some(row => row.name === process.argv[2]) ? 0 : 1)
    } catch { process.exit(2) }' "$1" "$2"
  else
    return 2
  fi
}
CODEX_FAIL=0
if command -v codex >/dev/null 2>&1; then
  for kind in plugin marketplace; do
    if [ "$kind" = plugin ]; then
      set -- plugin
      name=hackathonkit
      field=installed
    else
      set -- plugin marketplace
      name=hackathonkit-local
      field=marketplaces
    fi
    if ! listing=$(codex "$@" list --json 2>/dev/null); then
      echo "[uninstall] could not enumerate Codex $kind registrations" >&2
      CODEX_FAIL=1
      continue
    fi
    status=0
    printf '%s\n' "$listing" | registered "$field" "$name" || status=$?
    case "$status" in
      0) ;;
      1) continue ;;
      *) echo "[uninstall] cannot parse $kind registrations" >&2; CODEX_FAIL=1; continue ;;
    esac
    if [ "$APPLY" = "1" ]; then
      if ! codex "$@" remove "$name" >/dev/null 2>&1; then
        echo "[uninstall] could not remove Codex $kind $name" >&2
        CODEX_FAIL=1
      fi
    else
      echo "would run: codex $* remove $name"
    fi
  done
fi

# Remove the /verify custom prompt this installer placed for Codex.
if [ "$APPLY" = "1" ]; then
  if ! sh "$ROOT/scripts/install-prompts.sh" remove; then
    echo "[uninstall] warning: command prompt removal failed" >&2
    FAILURES=$((FAILURES + 1))
  fi
else
  echo "would remove: ${CODEX_HOME:-$HOME/.codex}/prompts/*.md (unmodified prompts only)"
fi

# Remove the managed hackathonkit block from the user-level Codex config.
if [ -f "$ROOT/scripts/gen-codex-config.sh" ]; then
  if [ "$APPLY" = "1" ]; then
    sh "$ROOT/scripts/gen-codex-config.sh" --remove || FAILURES=$((FAILURES + 1))
  else
    echo 'would remove: the managed hackathonkit block in ${CODEX_HOME:-$HOME/.codex}/config.toml'
  fi
fi

# Remove generated artifacts. FAILURES accumulates across every removal loop.
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
