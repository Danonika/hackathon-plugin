#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FAIL=0

check() {
  NAME="$1"
  OK="$2"
  DETAIL="$3"
  if [ "$OK" = "1" ]; then
    echo "[ok]   $NAME $DETAIL"
  else
    echo "[fail] $NAME $DETAIL"
    FAIL=$((FAIL + 1))
  fi
}

if [ -f "$ROOT/docker-compose.yml" ]; then check "repository root" 1 "$ROOT"; else check "repository root" 0 "$ROOT"; fi
if [ -f "$ROOT/configs/routing.yaml" ]; then check "routing config" 1 "$ROOT/configs/routing.yaml"; else check "routing config" 0 "missing"; fi

if command -v go >/dev/null 2>&1; then check "go toolchain (dev)" 1 "$(go version)"; else check "go toolchain (dev)" 1 "not found (not required for docker path)"; fi
if command -v codex >/dev/null 2>&1; then
  check "codex CLI" 1 "$(codex --version 2>/dev/null || echo present)"
  PLUGIN_STATE=missing
  PLUGIN_JSON=$(codex plugin list --json 2>/dev/null || true)
  if [ -n "$PLUGIN_JSON" ]; then
    if command -v jq >/dev/null 2>&1; then
      if printf '%s' "$PLUGIN_JSON" | jq -e '.installed[]? | select(.name == "hackathonkit")' >/dev/null 2>&1; then
        PLUGIN_STATE=disabled
        if printf '%s' "$PLUGIN_JSON" | jq -e '.installed[]? | select(.name == "hackathonkit" and (.enabled == true))' >/dev/null 2>&1; then
          PLUGIN_STATE=enabled
        fi
      fi
    elif printf '%s' "$PLUGIN_JSON" | grep -q '"name": *"hackathonkit"'; then
      PLUGIN_STATE=disabled
      if printf '%s' "$PLUGIN_JSON" | grep -q '"enabled": *true'; then
        PLUGIN_STATE=enabled
      fi
    fi
  fi
  PLUGIN_OK=0
  case "$PLUGIN_STATE" in
    enabled) check "hackathonkit plugin" 1 "installed and enabled"; PLUGIN_OK=1 ;;
    disabled) check "hackathonkit plugin" 0 "installed but disabled; enable it in Codex" ;;
    *) check "hackathonkit plugin" 0 "run scripts/setup.sh" ;;
  esac

  if [ "$PLUGIN_OK" = "1" ]; then
    PLUGIN_RUNTIME=0
    PLUGIN_CHECK_OUT=$(sh "$ROOT/scripts/plugin-check.sh" 2>&1) || PLUGIN_RUNTIME=$?
    case "$PLUGIN_RUNTIME" in
      0) check "plugin runtime" 1 "installed plugin starts MCP from a foreign cwd" ;;
      2) echo "[info] plugin runtime check skipped (codex/python3 unavailable)" ;;
      *) check "plugin runtime" 0 "$(printf '%s' "$PLUGIN_CHECK_OUT" | tail -n 1)" ;;
    esac
  else
    echo "[info] plugin runtime check skipped (plugin not installed or disabled)"
  fi
else
  check "codex CLI" 0 "not found"
fi

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  check "docker engine" 1 "running"
else
  check "docker engine" 0 "not running"
fi

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  check "mcp runtime" 1 "local binary"
elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker images --format '{{.Repository}}' 2>/dev/null | grep -q -- '-hackathonkit-mcp$'; then
  check "mcp runtime" 1 "docker image"
else
  check "mcp runtime" 0 "run scripts/setup.sh"
fi

SRC_COUNT=$(find "$ROOT/plugins/hackathonkit/skills" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
DST_COUNT=0
if [ -d "$ROOT/.agents/skills" ]; then
  DST_COUNT=$(find "$ROOT/.agents/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
fi
if [ "$SRC_COUNT" -gt 0 ] && [ "$SRC_COUNT" = "$DST_COUNT" ]; then
  check "skills mirror" 1 "plugin=$SRC_COUNT fallback=$DST_COUNT"
else
  check "skills mirror" 0 "plugin=$SRC_COUNT fallback=$DST_COUNT"
fi

if sh "$ROOT/scripts/smoke.sh" >/dev/null 2>&1; then
  check "mcp handshake" 1 "initialize / tools/list / system.context"
else
  check "mcp handshake" 0 "scripts/smoke.sh"
fi

if [ "$FAIL" = "0" ]; then
  echo "[doctor] all checks passed"
  exit 0
fi
echo "[doctor] $FAIL check(s) failed"
exit 1
