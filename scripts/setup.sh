#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"
echo "[setup] repository root: $ROOT"

if [ ! -f .env ]; then
  cp .env.example .env
  echo "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
fi

sh "$ROOT/scripts/sync-skills.sh"

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  docker compose -f "$ROOT/docker-compose.yml" build hackathonkit-mcp
  docker compose -f "$ROOT/docker-compose.yml" up -d postgres redis minio
  echo "[setup] MCP image built, infrastructure started"
else
  echo "[setup] docker engine unavailable; building local binaries instead"
  mkdir -p "$ROOT/bin"
  go build -o bin/hackathonkit-mcp ./cmd/hackathonkit-mcp
  go build -o bin/hackathonkit-demo-api ./cmd/demo-api
  echo "[setup] local binaries built in bin/"
fi

echo "[setup] done. Open Codex from the repository root, install the Hackathon Agent Kit plugin, start a NEW session."
