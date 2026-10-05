#!/usr/bin/env sh
# Staged Docker verification for the Hackathon Agent Kit stack.
#
#   scripts/docker-check.sh          # config, build, selftest, infra, smoke
#   scripts/docker-check.sh --fast   # default; same as no flag (accepted explicitly)
#   scripts/docker-check.sh --full   # also build and run the Go-enabled quality image
#   scripts/docker-check.sh --keep-infra  # leave postgres/redis/minio running
#
# Exit codes: 0 ok, 1 failed, 2 docker unavailable, 64 unknown argument.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$ROOT"

MODE="fast"
KEEP_INFRA=0
for arg in "$@"; do
  case "$arg" in
    --full) MODE="full" ;;
    --fast) MODE="fast" ;;
    --keep-infra) KEEP_INFRA=1 ;;
    -h|--help)
      echo "usage: scripts/docker-check.sh [--fast|--full] [--keep-infra]"
      exit 0
      ;;
    *)
      echo "[docker-check] unknown argument: $arg" >&2
      exit 64
      ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "[docker-check] docker CLI not found" >&2
  exit 2
fi
if ! docker info >/dev/null 2>&1; then
  echo "[docker-check] docker engine not reachable" >&2
  exit 2
fi

if [ ! -f .env ]; then
  cp .env.example .env
  echo "[docker-check] created .env from .env.example"
fi

# Containers run as HACKATHONKIT_UID:HACKATHONKIT_GID; align them with the current user
# so bind-mounted data/, logs/, evals/ and configs/ stay writable. env-set.sh
# preserves the .env permission bits (a plain rewrite would downgrade 0600).
sh "$ROOT/scripts/env-set.sh" .env HACKATHONKIT_UID "$(id -u)"
sh "$ROOT/scripts/env-set.sh" .env HACKATHONKIT_GID "$(id -g)"

mkdir -p logs evals data/artifacts data/index data/control data/judge-bundles

redact_logs() {
  # Single shared redactor (scripts/redact.sh); it fails closed if sed -E is
  # unavailable, so container logs are never printed unredacted.
  sh "$ROOT/scripts/redact.sh"
}

INFRA_STARTED=0
stop_infra() {
  if [ "$INFRA_STARTED" = "1" ]; then
    docker compose stop postgres redis minio >/dev/null 2>&1 || true
  fi
}

diagnostics() {
  status=$?
  # --keep-infra means the user wants to inspect the stack, so do not tear it
  # down on the failure path either.
  if [ "$KEEP_INFRA" != "1" ]; then
    stop_infra
  fi
  if [ "$status" -ne 0 ]; then
    echo "[docker-check] FAILED (exit $status); diagnostics (credentials redacted):" >&2
    docker compose ps -a 2>&1 | redact_logs >&2 || true
    docker compose logs --no-color --tail=120 2>&1 | redact_logs >&2 || true
    if [ "$MODE" = "full" ]; then
      docker compose --profile quality logs --no-color --tail=80 2>&1 | redact_logs >&2 || true
    fi
  fi
  exit "$status"
}
trap diagnostics EXIT

echo "[docker-check] compose config"
docker compose config --quiet

echo "[docker-check] build hackathonkit-mcp and demo-api"
docker compose build hackathonkit-mcp demo-api

if [ "$MODE" = "full" ]; then
  echo "[docker-check] build quality image"
  docker compose --profile quality build quality
fi

echo "[docker-check] start infrastructure (postgres, redis, minio)"
# Mark started before `up` so a partial failure still triggers teardown.
INFRA_STARTED=1
docker compose up -d postgres redis minio

echo "[docker-check] wait for postgres and minio"
tries=0
until docker compose exec -T postgres pg_isready -U hackathonkit -d hackathonkit >/dev/null 2>&1; do
  tries=$((tries + 1))
  if [ "$tries" -ge 30 ]; then
    echo "[docker-check] postgres did not become ready" >&2
    exit 1
  fi
  sleep 2
done
echo "[docker-check] wait for redis"
tries=0
until docker compose exec -T redis redis-cli ping 2>/dev/null | grep -q PONG; do
  tries=$((tries + 1))
  if [ "$tries" -ge 30 ]; then
    echo "[docker-check] redis did not become ready" >&2
    exit 1
  fi
  sleep 2
done

# Derive the host port from the compose published mapping. HACKATHONKIT_MINIO_PORT
# is not referenced by docker-compose.yml, so an exported value would probe a
# port unrelated to the one compose actually publishes.
MINIO_PORT_OUTPUT=$(docker compose port minio 9000 2>&1 || true)
MINIO_PORT=$(printf '%s\n' "$MINIO_PORT_OUTPUT" | sed -n 's/.*:\([0-9][0-9]*\)$/\1/p' | head -n 1)
if [ -z "$MINIO_PORT" ]; then
  # Do not guess 9000: probing an unrelated service bound to that port could
  # produce a false pass. Surface the raw output and fail loudly instead.
  echo "[docker-check] could not resolve the published minio port from 'docker compose port minio 9000':" >&2
  printf '%s\n' "$MINIO_PORT_OUTPUT" >&2
  exit 1
fi
if command -v curl >/dev/null 2>&1; then
  tries=0
  until curl -fsS "http://127.0.0.1:${MINIO_PORT}/minio/health/live" >/dev/null 2>&1; do
    tries=$((tries + 1))
    if [ "$tries" -ge 30 ]; then
      echo "[docker-check] minio did not become ready" >&2
      exit 1
    fi
    sleep 2
  done
else
  echo "[docker-check] curl not found; probing the minio health endpoint from inside the stack"
  MINIO_OK=0
  tries=0
  while [ "$tries" -lt 30 ]; do
    tries=$((tries + 1))
    if docker compose exec -T minio sh -c 'curl -fsS http://127.0.0.1:9000/minio/health/live >/dev/null 2>&1 || wget -q -O- http://127.0.0.1:9000/minio/health/live >/dev/null 2>&1' >/dev/null 2>&1; then
      MINIO_OK=1
      break
    fi
    if docker compose exec -T postgres sh -c 'wget -q -O- http://minio:9000/minio/health/live >/dev/null 2>&1' >/dev/null 2>&1; then
      MINIO_OK=1
      break
    fi
    sleep 2
  done
  if [ "$MINIO_OK" != "1" ]; then
    echo "[docker-check] minio health endpoint did not become ready" >&2
    exit 1
  fi
fi

echo "[docker-check] MCP selftest in the container"
docker compose run --rm -T hackathonkit-mcp --selftest >/dev/null

if [ "$MODE" = "full" ]; then
  echo "[docker-check] quality gate in the Go-enabled image"
  docker compose --profile quality run --rm -T quality
fi

echo "[docker-check] MCP handshake over compose"
sh scripts/smoke.sh --compose

if [ "$KEEP_INFRA" = "1" ]; then
  echo "[docker-check] leaving infrastructure running (--keep-infra)"
else
  stop_infra
  INFRA_STARTED=0
fi
trap - EXIT
echo "[docker-check] ok (mode: $MODE)"
