#!/usr/bin/env sh
# Regenerate missing generated artifacts without rebuilding infrastructure.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

echo "[repair] repository root: $ROOT"

sh "$ROOT/scripts/sync-harness.sh"
sh "$ROOT/scripts/gen-mcp-manifest.sh" "$ROOT"
mkdir -p "$ROOT/logs" "$ROOT/evals" "$ROOT/data/artifacts" "$ROOT/data/index" "$ROOT/data/control" "$ROOT/data/judge-bundles"

PROFILE="full"
if [ -f "$ROOT/configs/install-state.json" ]; then
  DETECTED=$(sed -n 's/^[[:space:]]*"profile": *"\([^"]*\)".*/\1/p' "$ROOT/configs/install-state.json" | head -n 1)
  if [ -n "$DETECTED" ]; then
    PROFILE="$DETECTED"
  else
    echo "[repair] warning: could not read the profile; using 'full'" >&2
  fi
fi
RUNTIME="binary"
if command -v docker >/dev/null 2>&1; then
  if command -v timeout >/dev/null 2>&1; then
    if timeout 5 docker info >/dev/null 2>&1; then
      RUNTIME="docker"
    fi
  elif docker info >/dev/null 2>&1; then
    RUNTIME="docker"
  fi
fi

if { [ ! -x "$ROOT/bin/hackathonkit-mcp" ] || [ ! -x "$ROOT/bin/hackathonkit-demo-api" ] || [ ! -x "$ROOT/bin/hackathonkit-ctl" ]; } && [ "$RUNTIME" = "binary" ]; then
  if command -v go >/dev/null 2>&1; then
    mkdir -p "$ROOT/bin"
    (
      cd "$ROOT"
      go build -o "$ROOT/bin/hackathonkit-mcp.tmp" ./cmd/hackathonkit-mcp &&
        go build -o "$ROOT/bin/hackathonkit-demo-api.tmp" ./cmd/demo-api &&
        go build -o "$ROOT/bin/hackathonkit-ctl.tmp" ./cmd/hackathonkit-ctl
    ) || {
      rm -f "$ROOT/bin/hackathonkit-mcp.tmp" "$ROOT/bin/hackathonkit-demo-api.tmp" "$ROOT/bin/hackathonkit-ctl.tmp"
      echo "[repair] go build failed" >&2
      exit 1
    }
    mv "$ROOT/bin/hackathonkit-mcp.tmp" "$ROOT/bin/hackathonkit-mcp"
    mv "$ROOT/bin/hackathonkit-demo-api.tmp" "$ROOT/bin/hackathonkit-demo-api"
    mv "$ROOT/bin/hackathonkit-ctl.tmp" "$ROOT/bin/hackathonkit-ctl"
    echo "[repair] rebuilt local binaries in bin/"
  else
    echo "[repair] no Go toolchain and no docker engine; MCP runtime unavailable" >&2
    exit 1
  fi
fi

sh "$ROOT/scripts/write-install-state.sh" "$ROOT" "$PROFILE" "$RUNTIME"

if command -v codex >/dev/null 2>&1; then
  if ! codex plugin marketplace add "$ROOT" >/dev/null 2>&1; then
    echo "[repair] warning: codex plugin marketplace add failed" >&2
  fi
  if codex plugin add hackathonkit@hackathonkit-local >/dev/null 2>&1; then
    echo "[repair] plugin reinstalled; start a NEW Codex session"
  else
    echo "[repair] warning: could not reinstall the plugin automatically" >&2
    exit 1
  fi
fi

echo "[repair] done. Run scripts/doctor.sh to verify."
