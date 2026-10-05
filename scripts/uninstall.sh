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
          rm -rf "$mirror/$name" 2>/dev/null || FAILURES=$((FAILURES + 1))
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

CODEX_FAIL=0
if ! command -v codex >/dev/null 2>&1; then
  echo "[uninstall] codex CLI not found; skipping plugin/marketplace removal"
elif [ "$APPLY" = "1" ]; then
  # A removal failure is only a real problem when the plugin/marketplace was
  # actually registered; a benign "not installed" no-op must not fail the run.
  # Boundary-anchored so a plugin whose name merely contains "hackathonkit"
  # (hackathonkit-tools, my-hackathonkit) cannot trigger a destructive remove.
  if codex plugin list 2>/dev/null | grep -Eq '(^|[^A-Za-z0-9_-])hackathonkit([^A-Za-z0-9_-]|$)'; then
    if ! codex plugin remove hackathonkit >/dev/null 2>&1; then
      echo "[uninstall] warning: codex plugin remove failed" >&2
      CODEX_FAIL=1
    fi
  else
    echo "[uninstall] hackathonkit plugin is not registered; skipping plugin remove"
  fi
  if MARKETPLACES=$(codex plugin marketplace list 2>/dev/null); then
    # Boundary-anchored: hackathonkit-local-v2 must not match the exact name.
    if printf '%s' "$MARKETPLACES" | grep -Eq '(^|[^A-Za-z0-9_-])hackathonkit-local([^A-Za-z0-9_-]|$)'; then
      if ! codex plugin marketplace remove hackathonkit-local >/dev/null 2>&1; then
        echo "[uninstall] warning: codex marketplace remove failed" >&2
        CODEX_FAIL=1
      fi
    else
      echo "[uninstall] hackathonkit-local marketplace is not registered; skipping marketplace remove"
    fi
  elif ! codex plugin marketplace remove hackathonkit-local >/dev/null 2>&1; then
    # Best-effort: `marketplace list` failed, so a remove failure here most
    # likely means it was never registered; do not fail the run for it.
    echo "[uninstall] note: could not remove hackathonkit-local marketplace (best-effort; it may not be registered)" >&2
  fi
  if [ "$CODEX_FAIL" -eq 0 ]; then
    echo "[uninstall] removed hackathonkit plugin and marketplace"
  else
    echo "[uninstall] plugin/marketplace removal reported warnings" >&2
  fi
else
  # Mirror the apply-path conditions (registration checks) so the preview does
  # not advertise a removal that apply would actually skip.
  if codex plugin list 2>/dev/null | grep -Eq '(^|[^A-Za-z0-9_-])hackathonkit([^A-Za-z0-9_-]|$)'; then
    echo "would run: codex plugin remove hackathonkit"
  else
    echo "[uninstall] hackathonkit plugin is not registered; skipping plugin remove"
  fi
  if MARKETPLACES=$(codex plugin marketplace list 2>/dev/null); then
    if printf '%s' "$MARKETPLACES" | grep -Eq '(^|[^A-Za-z0-9_-])hackathonkit-local([^A-Za-z0-9_-]|$)'; then
      echo "would run: codex plugin marketplace remove hackathonkit-local"
    else
      echo "[uninstall] hackathonkit-local marketplace is not registered; skipping marketplace remove"
    fi
  else
    echo "would run: codex plugin marketplace remove hackathonkit-local (marketplace list unavailable)"
  fi
fi

# Remove the /verify custom prompt this installer placed for Codex.
if [ "$APPLY" = "1" ]; then
  if ! sh "$ROOT/scripts/install-prompts.sh" remove; then
    echo "[uninstall] warning: command prompt removal failed" >&2
    FAILURES=$((FAILURES + 1))
  fi
else
  echo "would remove: $HOME/.codex/prompts/*.md (unmodified prompts only)"
fi

# Remove the managed hackathonkit block from the user-level Codex config.
if [ -f "$ROOT/scripts/gen-codex-config.sh" ]; then
  if [ "$APPLY" = "1" ]; then
    sh "$ROOT/scripts/gen-codex-config.sh" --remove >/dev/null 2>&1 || true
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
