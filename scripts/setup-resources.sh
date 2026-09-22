#!/usr/bin/env sh
# Interactive resource questionnaire: asks which API resources the user has,
# records booleans in configs/resources.json and stores any keys in .env with
# scripts/env-set.sh (0600). Secrets never leave .env; the profile is booleans.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
MCP="$ROOT/bin/hackathonkit-mcp"

mcp() {
  if [ -x "$MCP" ]; then
    "$MCP" --repo-root "$ROOT" "$@"
  else
    (cd "$ROOT" && go run ./cmd/hackathonkit-mcp --repo-root "$ROOT" "$@")
  fi
}

ask_yes() {
  printf '%s [y/N] ' "$1"
  read -r answer || answer=""
  case "$answer" in y|Y|yes|YES) return 0 ;; *) return 1 ;; esac
}

restore_echo() {
  if [ -t 0 ] && command -v stty >/dev/null 2>&1; then
    stty echo 2>/dev/null || true
  fi
}

# read_secret prints its prompt to stderr so command substitution captures only
# the value; the value itself is returned on stdout.
read_secret() {
  printf '%s: ' "$1" >&2
  if [ -t 0 ] && command -v stty >/dev/null 2>&1; then stty -echo; fi
  read -r value || value=""
  if [ -t 0 ] && command -v stty >/dev/null 2>&1; then
    stty echo
    printf '\n' >&2
  fi
  printf '%s' "$value"
}

# set_key feeds the value through stdin so it never appears in process argv.
# Returns non-zero when no value was supplied, so the caller can avoid
# claiming a resource whose key was not stored.
set_key() {
  [ -n "$2" ] || return 1
  printf '%s' "$2" | sh "$ROOT/scripts/env-set.sh" "$ROOT/.env" "$1"
}

on_interrupt() {
  restore_echo
  exit 130
}
trap restore_echo EXIT
trap on_interrupt INT TERM

if [ ! -t 0 ]; then
  echo "[resources] non-interactive stdin: every unanswered prompt defaults to no" >&2
fi
echo "[resources] answering stores booleans in configs/resources.json; keys go to .env only"

OPENAI=0; NVIDIA=0; TELEGRAM=0; POSTGRES=0; MINIO=0; CODEX_PRO=0
if ask_yes "Do you have an OpenAI API key (chat, vision, whisper, embeddings)?"; then OPENAI=1; fi
if ask_yes "Do you have an NVIDIA API key (LLM, ASR/voices, vision)?"; then NVIDIA=1; fi
if ask_yes "Do you have a Telegram bot token for project notifications?"; then TELEGRAM=1; fi
if ask_yes "Do you have a Postgres DSN for the control plane?"; then POSTGRES=1; fi
if ask_yes "Do you have MinIO credentials for artifact mirroring?"; then MINIO=1; fi
if ask_yes "Do you have a ChatGPT/Codex Pro subscription?"; then CODEX_PRO=1; fi

printf 'Track (number 01-10 or slug, empty to skip): '
read -r TRACK || TRACK=""
case "$TRACK" in
  ""|[0-9]|[0-9][0-9]|[0-9][0-9]-[a-z-]*|[a-z][a-z-]*) ;;
  *)
    echo "[resources] ignoring invalid track '$TRACK' (expected e.g. 08 or 08-innovation)" >&2
    TRACK=""
    ;;
esac

# Collect secrets first and only claim a provider when its key was stored.
if [ "$OPENAI" = 1 ] && ! set_key OPENAI_API_KEY "$(read_secret 'OPENAI_API_KEY (empty to skip)')"; then
  OPENAI=0
fi
if [ "$NVIDIA" = 1 ] && ! set_key NVIDIA_API_KEY "$(read_secret 'NVIDIA_API_KEY (empty to skip)')"; then
  NVIDIA=0
fi
if [ "$TELEGRAM" = 1 ]; then
  if ! set_key TELEGRAM_BOT_TOKEN "$(read_secret 'TELEGRAM_BOT_TOKEN (empty to skip)')"; then
    TELEGRAM=0
  else
    set_key TELEGRAM_CHAT_ID "$(read_secret 'TELEGRAM_CHAT_ID (empty to skip)')" || true
  fi
fi
if [ "$POSTGRES" = 1 ] && ! set_key POSTGRES_DSN "$(read_secret 'POSTGRES_DSN (empty to skip)')"; then
  POSTGRES=0
fi
if [ "$MINIO" = 1 ] && ! set_key MINIO_ENDPOINT "$(read_secret 'MINIO_ENDPOINT (empty to skip)')"; then
  MINIO=0
fi

SPEC="openai_api=$([ "$OPENAI" = 1 ] && echo true || echo false)"
SPEC="$SPEC,nvidia_api=$([ "$NVIDIA" = 1 ] && echo true || echo false)"
SPEC="$SPEC,telegram=$([ "$TELEGRAM" = 1 ] && echo true || echo false)"
SPEC="$SPEC,postgres=$([ "$POSTGRES" = 1 ] && echo true || echo false)"
SPEC="$SPEC,minio=$([ "$MINIO" = 1 ] && echo true || echo false)"
SPEC="$SPEC,codex_pro=$([ "$CODEX_PRO" = 1 ] && echo true || echo false)"
if [ -n "$TRACK" ]; then
  SPEC="$SPEC,track=$TRACK"
fi

mcp --resources-set "$SPEC" >/dev/null

echo "[resources] saved profile:"
mcp --resources
