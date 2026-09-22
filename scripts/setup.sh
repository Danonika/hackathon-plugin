#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

PROFILE="full"
while [ $# -gt 0 ]; do
  case "$1" in
    --profile)
      PROFILE="${2:-}"
      if [ -z "$PROFILE" ]; then
        echo "[setup] --profile requires a value (core or full)" >&2
        exit 1
      fi
      shift 2
      ;;
    --profile=*)
      PROFILE="${1#*=}"
      shift
      ;;
    -h|--help)
      echo "usage: scripts/setup.sh [--profile core|full]"
      exit 0
      ;;
    *)
      echo "[setup] unknown argument: $1" >&2
      exit 1
      ;;
  esac
done
case "$PROFILE" in
  core|full) ;;
  *)
    echo "[setup] invalid profile: $PROFILE (expected core or full)" >&2
    exit 1
    ;;
esac

cd "$ROOT"
echo "[setup] repository root: $ROOT (profile: $PROFILE)"

if [ ! -f .env ]; then
  (umask 077 && cp .env.example .env)
  chmod 600 .env 2>/dev/null || true
  echo "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
fi

# Persist the host UID/GID so non-root containers can write through bind mounts.
if command -v id >/dev/null 2>&1; then
  sh "$ROOT/scripts/env-set.sh" .env HACKATHONKIT_UID "$(id -u)"
  sh "$ROOT/scripts/env-set.sh" .env HACKATHONKIT_GID "$(id -g)"
  echo "[setup] recorded HACKATHONKIT_UID/HACKATHONKIT_GID in .env for bind-mount ownership"
fi

sh "$ROOT/scripts/sync-harness.sh"

if [ "$PROFILE" = "core" ]; then
  for dir in "$ROOT/.agents/skills" "$ROOT/.claude/skills"; do
    [ -d "$dir" ] || continue
    for skill in judge project-cost roast-project cp-expert-refactoring system-design-expert self-improvement-controller output-self-check skill-authoring qa-expert; do
      rm -rf "$dir/$skill"
    done
  done
  echo "[setup] core profile: explicit-only evaluation skills excluded from mirrors (the plugin still ships all skills)"
fi

sh "$ROOT/scripts/gen-mcp-manifest.sh" "$ROOT"

# Pre-create writable mount points as the host user so the non-root
# containers can write logs and artifacts through the bind mounts.
mkdir -p "$ROOT/logs" "$ROOT/evals" "$ROOT/data/artifacts" "$ROOT/data/index" "$ROOT/data/control" "$ROOT/data/judge-bundles"

RUNTIME="binary"
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  if docker compose -f "$ROOT/docker-compose.yml" build hackathonkit-mcp &&
    docker compose -f "$ROOT/docker-compose.yml" up -d postgres redis minio; then
    RUNTIME="docker"
    echo "[setup] MCP image built, infrastructure started"
  else
    echo "[setup] docker path failed; falling back to local binaries" >&2
  fi
fi

if [ "$RUNTIME" = "binary" ]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "[setup] no docker engine and no Go toolchain; install Go or start Docker" >&2
    exit 1
  fi
  echo "[setup] building local binaries"
  mkdir -p "$ROOT/bin"
  (cd "$ROOT" && go build -o bin/hackathonkit-mcp ./cmd/hackathonkit-mcp)
  (cd "$ROOT" && go build -o bin/hackathonkit-demo-api ./cmd/demo-api)
  (cd "$ROOT" && go build -o bin/hackathonkit-ctl ./cmd/hackathonkit-ctl)
  echo "[setup] local binaries built in bin/"
fi

sh "$ROOT/scripts/write-install-state.sh" "$ROOT" "$PROFILE" "$RUNTIME"

if command -v codex >/dev/null 2>&1; then
  echo "[setup] registering repo marketplace and installing the Hackathon Agent Kit plugin"
  codex plugin remove hackathonkit >/dev/null 2>&1 || true
  codex plugin marketplace add "$ROOT" >/dev/null 2>&1 || true
  if codex plugin add hackathonkit@hackathonkit-local >/dev/null 2>&1; then
    echo "[setup] plugin installed; start a NEW Codex session to load it"
  else
    echo "[setup] could not install the plugin automatically. Run manually: codex plugin marketplace add \"$ROOT\" && codex plugin add hackathonkit@hackathonkit-local" >&2
  fi
else
  echo "[setup] codex CLI not found; install it (npm install -g @openai/codex) and re-run setup" >&2
fi

echo "[setup] done. Open Codex from the repository root and start a NEW Codex session."
