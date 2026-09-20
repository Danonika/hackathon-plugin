#!/usr/bin/env sh
# Regenerate missing generated artifacts without rebuilding infrastructure.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

echo "[repair] repository root: $ROOT"

sh "$ROOT/scripts/sync-harness.sh"
sh "$ROOT/scripts/gen-mcp-manifest.sh" "$ROOT"
mkdir -p "$ROOT/logs" "$ROOT/data/artifacts" "$ROOT/data/index" "$ROOT/data/control"

PROFILE="full"
if [ -f "$ROOT/configs/install-state.json" ]; then
  DETECTED=$(sed -n 's/.*"profile": *"\([^"]*\)".*/\1/p' "$ROOT/configs/install-state.json" | head -n 1)
  if [ -n "$DETECTED" ]; then
    PROFILE="$DETECTED"
  fi
fi
RUNTIME="binary"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  RUNTIME="docker"
fi

if [ ! -x "$ROOT/bin/hackathonkit-mcp" ] && [ "$RUNTIME" = "binary" ]; then
  if command -v go >/dev/null 2>&1; then
    mkdir -p "$ROOT/bin"
    go build -o "$ROOT/bin/hackathonkit-mcp" ./cmd/hackathonkit-mcp
    go build -o "$ROOT/bin/hackathonkit-demo-api" ./cmd/demo-api
    echo "[repair] rebuilt local binaries in bin/"
  else
    echo "[repair] warning: no Go toolchain and no docker engine; MCP runtime unavailable" >&2
  fi
fi

sh "$ROOT/scripts/write-install-state.sh" "$ROOT" "$PROFILE" "$RUNTIME"

if command -v codex >/dev/null 2>&1; then
  codex plugin marketplace add "$ROOT" >/dev/null 2>&1 || true
  if codex plugin add hackathonkit@hackathonkit-local >/dev/null 2>&1; then
    echo "[repair] plugin reinstalled; start a NEW Codex session"
  else
    echo "[repair] warning: could not reinstall the plugin automatically" >&2
  fi
fi

echo "[repair] done. Run scripts/doctor.sh to verify."
