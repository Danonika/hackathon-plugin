#!/usr/bin/env sh
# Regenerate missing generated artifacts without rebuilding infrastructure.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

echo "[repair] repository root: $ROOT"

sh "$ROOT/scripts/sync-harness.sh"
sh "$ROOT/scripts/gen-mcp-manifest.sh" "$ROOT"
sh "$ROOT/scripts/install-prompts.sh" install || echo "[repair] warning: could not install the Codex command prompts" >&2
mkdir -p "$ROOT/logs" "$ROOT/evals" "$ROOT/data/artifacts" "$ROOT/data/index" "$ROOT/data/control" "$ROOT/data/judge-bundles"

PROFILE="full"
if [ -f "$ROOT/configs/install-state.json" ]; then
  DETECTED=$(sed -n 's/^[[:space:]]*"profile": *"\([^"]*\)".*/\1/p' "$ROOT/configs/install-state.json" | head -n 1)
  case "$DETECTED" in
    core|full) PROFILE="$DETECTED" ;;
    "") echo "[repair] warning: could not read the profile; using 'full'" >&2 ;;
    *) echo "[repair] warning: unknown profile '$DETECTED' in install-state; using 'full'" >&2 ;;
  esac
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
    MCP_TMP=$(mktemp "$ROOT/bin/hackathonkit-mcp.tmp.XXXXXX")
    DEMO_TMP=$(mktemp "$ROOT/bin/hackathonkit-demo-api.tmp.XXXXXX")
    CTL_TMP=$(mktemp "$ROOT/bin/hackathonkit-ctl.tmp.XXXXXX")
    cleanup_bin() { rm -f "$MCP_TMP" "$DEMO_TMP" "$CTL_TMP"; }
    (
      cd "$ROOT"
      go build -o "$MCP_TMP" ./cmd/hackathonkit-mcp &&
        go build -o "$DEMO_TMP" ./cmd/demo-api &&
        go build -o "$CTL_TMP" ./cmd/hackathonkit-ctl
    ) || {
      cleanup_bin
      echo "[repair] go build failed" >&2
      exit 1
    }
    mv "$MCP_TMP" "$ROOT/bin/hackathonkit-mcp"
    mv "$DEMO_TMP" "$ROOT/bin/hackathonkit-demo-api"
    mv "$CTL_TMP" "$ROOT/bin/hackathonkit-ctl"
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
  if codex plugin add hackathonkit@hackathonkit-local >/dev/null 2>&1 || codex plugin list 2>/dev/null | grep -q hackathonkit; then
    echo "[repair] plugin present; start a NEW Codex session"
  else
    echo "[repair] warning: could not reinstall the plugin automatically" >&2
    exit 1
  fi
fi

echo "[repair] done. Run $ROOT/scripts/doctor.sh to verify."
