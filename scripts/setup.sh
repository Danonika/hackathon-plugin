#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"
echo "[setup] repository root: $ROOT"

if [ ! -f .env ]; then
  cp .env.example .env
  echo "[setup] created .env from .env.example (set OPENAI_API_KEY for speech/vision)"
fi

# Persist the host UID/GID so non-root containers can write through bind mounts.
if command -v id >/dev/null 2>&1 && ! grep -q '^HACKATHONKIT_UID=' .env 2>/dev/null; then
  printf 'HACKATHONKIT_UID=%s\nHACKATHONKIT_GID=%s\n' "$(id -u)" "$(id -g)" >> .env
  echo "[setup] recorded HACKATHONKIT_UID/HACKATHONKIT_GID in .env for bind-mount ownership"
fi

sh "$ROOT/scripts/sync-skills.sh"

TEMPLATE="$ROOT/plugins/hackathonkit/.mcp.json.template"
if [ ! -f "$TEMPLATE" ]; then
  echo "[setup] template not found: $TEMPLATE" >&2
  exit 1
fi
TMP_MANIFEST="$ROOT/plugins/hackathonkit/.mcp.json.tmp.$$"
cleanup_manifest() { rm -f "$TMP_MANIFEST"; }
trap cleanup_manifest EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
awk -v root="$ROOT" '{
  while ((i = index($0, "{{REPO_ROOT}}")) > 0) {
    $0 = substr($0, 1, i - 1) root substr($0, i + 13)
  }
  print
}' "$TEMPLATE" > "$TMP_MANIFEST"
mv "$TMP_MANIFEST" "$ROOT/plugins/hackathonkit/.mcp.json"
echo "[setup] generated plugins/hackathonkit/.mcp.json with the absolute repository root"

# Pre-create writable mount points as the host user so the non-root
# containers can write logs and artifacts through the bind mounts.
mkdir -p "$ROOT/logs" "$ROOT/data/artifacts" "$ROOT/data/index"

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
