#!/usr/bin/env sh
# Regenerate missing generated artifacts without rebuilding infrastructure.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

echo "[repair] repository root: $ROOT"

# Serialize concurrent repairs: two runs can race on the temp binaries and the
# install-state write. Note that replacing a binary in place while an MCP/demo
# process is running causes version skew until that process is restarted.
mkdir -p "$ROOT/data/locks"
. "$ROOT/scripts/file-lock.sh"
acquire_file_lock "$ROOT/data/locks/repair.lock"
build_stage=""
trap '[ -z "$build_stage" ] || rm -rf "$build_stage"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

. "$ROOT/scripts/skill-profile.sh"
PROFILE=$(installed_skill_profile "$ROOT")
sh "$ROOT/scripts/sync-harness.sh" "$PROFILE"
sh "$ROOT/scripts/gen-mcp-manifest.sh" "$ROOT"
sh "$ROOT/scripts/gen-codex-config.sh" "$ROOT"
sh "$ROOT/scripts/install-prompts.sh" install
mkdir -p "$ROOT/logs" "$ROOT/evals" "$ROOT/data/artifacts" "$ROOT/data/index" "$ROOT/data/control" "$ROOT/data/judge-bundles"

RUNTIME="binary"
# Only probe docker when the probe can be bounded; an unbounded `docker info`
# can hang the repair on hosts without `timeout` (e.g. macOS).
if command -v docker >/dev/null 2>&1 && ! command -v timeout >/dev/null 2>&1; then
  # Surface the degradation: without `timeout` the probe could hang, so docker
  # is ignored and a local rebuild is forced even when a usable image exists.
  echo "[repair] warning: docker is present but timeout(1) is unavailable; skipping the docker probe and using local binaries" >&2
elif command -v docker >/dev/null 2>&1 && command -v timeout >/dev/null 2>&1; then
  if timeout 5 docker info >/dev/null 2>&1; then
    # A live daemon is not sufficient: the built MCP image must exist, otherwise
    # the Go rebuild is skipped and the MCP runtime stays unavailable.
    if timeout 10 docker image inspect hackathon-agent-kit-hackathonkit-mcp >/dev/null 2>&1; then
      RUNTIME="docker"
    else
      echo "[repair] warning: docker is up but the hackathonkit-mcp image is missing; using local binaries" >&2
    fi
  fi
fi

# Bring older complete binary installations up to the launcher's private-write
# policy even when all three executables already exist and no rebuild is needed.
. "$ROOT/scripts/private-write.sh"
if [ "$RUNTIME" = binary ] && [ -e "$ROOT/bin" ]; then
  [ -d "$ROOT/bin" ] && [ ! -L "$ROOT/bin" ] || { echo "[repair] refusing non-directory or linked bin/" >&2; exit 1; }
  private_write "$ROOT/bin"
  for name in hackathonkit-mcp hackathonkit-demo-api hackathonkit-ctl; do
    binary="$ROOT/bin/$name"
    [ ! -L "$binary" ] || { echo "[repair] refusing linked binary: $name" >&2; exit 1; }
    if [ -f "$binary" ]; then private_write "$binary"; fi
  done
fi

if { [ ! -x "$ROOT/bin/hackathonkit-mcp" ] || [ ! -x "$ROOT/bin/hackathonkit-demo-api" ] || [ ! -x "$ROOT/bin/hackathonkit-ctl" ]; } && [ "$RUNTIME" = "binary" ]; then
  if command -v go >/dev/null 2>&1; then
    mkdir -p "$ROOT/bin"
    private_write "$ROOT/bin"
    build_stage=$(mktemp -d "$ROOT/bin/.repair.XXXXXX")
    MCP_TMP="$build_stage/hackathonkit-mcp"
    DEMO_TMP="$build_stage/hackathonkit-demo-api"
    CTL_TMP="$build_stage/hackathonkit-ctl"
    (
      cd "$ROOT"
      go build -trimpath -buildvcs=false -o "$MCP_TMP" ./cmd/hackathonkit-mcp &&
        go build -trimpath -buildvcs=false -o "$DEMO_TMP" ./cmd/demo-api &&
        go build -trimpath -buildvcs=false -o "$CTL_TMP" ./cmd/hackathonkit-ctl
    ) || {
      echo "[repair] go build failed" >&2
      exit 1
    }
    # mktemp creates 0600 files and `go build -trimpath -buildvcs=false -o <existing file>` keeps that
    # mode, so an explicit chmod is required before the binaries are usable.
    chmod 0755 "$MCP_TMP" "$DEMO_TMP" "$CTL_TMP"
    mv "$MCP_TMP" "$ROOT/bin/hackathonkit-mcp"
    mv "$DEMO_TMP" "$ROOT/bin/hackathonkit-demo-api"
    mv "$CTL_TMP" "$ROOT/bin/hackathonkit-ctl"
    chmod 0755 "$ROOT/bin/hackathonkit-mcp" "$ROOT/bin/hackathonkit-demo-api" "$ROOT/bin/hackathonkit-ctl"
    echo "[repair] rebuilt local binaries in bin/"
  else
    echo "[repair] no Go toolchain and no docker engine; MCP runtime unavailable" >&2
    exit 1
  fi
fi

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

# Persist install state only after every step (including the Codex verification
# above) has succeeded, so a failed run cannot claim a completed repair.
sh "$ROOT/scripts/write-install-state.sh" "$ROOT" "$PROFILE" "$RUNTIME"

echo "[repair] done. Run $ROOT/scripts/doctor.sh to verify."
