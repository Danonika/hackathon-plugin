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
  if codex plugin list --json 2>/dev/null | grep -q '"name": *"hackathonkit"'; then
    check "hackathonkit plugin" 1 "installed (check enabled in Codex)"
  else
    check "hackathonkit plugin" 0 "run scripts/setup.sh"
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
elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1 && docker images -q hackathon-agent-kit-hackathonkit-mcp | grep -q .; then
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
