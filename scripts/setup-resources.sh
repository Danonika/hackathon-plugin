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
  # IFS= preserves a value that legitimately starts/ends with blanks.
  IFS= read -r value || value=""
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

# Publish multi-key provider credentials as one update. Cooperate with individual
# env-set writes on the live path while the helper edits a private staging file.
set_provider_keys() (
  env_file="$ROOT/.env"
  . "$ROOT/scripts/file-lock.sh"
  acquire_file_lock "$env_file.write-lock" || exit 1
  if [ -L "$env_file" ] || { [ -e "$env_file" ] && [ ! -f "$env_file" ]; }; then
    echo "[resources] refusing a non-regular .env" >&2
    exit 1
  fi
  stage=$(mktemp "$env_file.provider.XXXXXX") || exit 1
  trap 'rm -f "$stage" "$stage.write-lock"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  if [ -f "$env_file" ]; then cp "$env_file" "$stage" || exit 1; fi
  while [ "$#" -gt 0 ]; do
    printf '%s' "$2" | sh "$ROOT/scripts/env-set.sh" "$stage" "$1" || exit 1
    shift 2
  done
  mv "$stage" "$env_file"
)

on_interrupt() {
  restore_echo
  exit 130
}
trap restore_echo EXIT
# HUP/QUIT too: if the shell dies while `read_secret` has echo disabled the
# terminal would otherwise be left without echo.
trap on_interrupt INT TERM HUP QUIT

if [ ! -t 0 ]; then
  echo "[resources] non-interactive stdin: prompts read from the pipe and unanswered/EOF prompts default to no; secrets are only stored when supplied" >&2
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
  "") ;;
  [0-9]|[0-9][0-9]) ;;
  # Only the known catalog slugs are accepted, so a typo like 08-foo is rejected
  # here instead of failing later in `mcp --resources-set` after secrets are set.
  01-energy|02-finance|03-management|04-telecom|05-logistics|06-creative-media|07-education|08-innovation|09-communications|10-retail) ;;
  *)
    echo "[resources] ignoring invalid track '$TRACK' (expected 01-10 or 08-innovation)" >&2
    TRACK=""
    ;;
esac
if [ -n "$TRACK" ]; then
  NUM=${TRACK%%-*}
  NUM=${NUM#0}
  [ -z "$NUM" ] && NUM=0
  case "$NUM" in
    ''|*[!0-9]*) echo "[resources] ignoring invalid track '$TRACK'" >&2; TRACK="" ;;
    *) if [ "$NUM" -lt 1 ] || [ "$NUM" -gt 10 ]; then
         echo "[resources] ignoring out-of-range track '$TRACK' (expected 01-10)" >&2
         TRACK=""
       fi ;;
  esac
fi

# Collect secrets first and only claim a provider when its key was stored.
if [ "$OPENAI" = 1 ] && ! set_key OPENAI_API_KEY "$(read_secret 'OPENAI_API_KEY (empty to skip)')"; then
  OPENAI=0
fi
if [ "$NVIDIA" = 1 ] && ! set_key NVIDIA_API_KEY "$(read_secret 'NVIDIA_API_KEY (empty to skip)')"; then
  NVIDIA=0
fi
if [ "$TELEGRAM" = 1 ]; then
  bot_token=$(read_secret 'TELEGRAM_BOT_TOKEN (empty to skip)')
  chat_id=""
  if [ -n "$bot_token" ]; then chat_id=$(read_secret 'TELEGRAM_CHAT_ID (empty to skip)'); fi
  if [ -n "$bot_token" ] && [ -n "$chat_id" ]; then
    set_provider_keys TELEGRAM_BOT_TOKEN "$bot_token" TELEGRAM_CHAT_ID "$chat_id"
  else
    TELEGRAM=0
  fi
  unset bot_token chat_id
fi
if [ "$POSTGRES" = 1 ] && ! set_key POSTGRES_DSN "$(read_secret 'POSTGRES_DSN (empty to skip)')"; then
  POSTGRES=0
fi
if [ "$MINIO" = 1 ]; then
  # MinIO needs all three keys; claim it only when the full set was stored,
  # otherwise resources.json would advertise a provider that cannot authenticate.
  endpoint=$(read_secret 'MINIO_ENDPOINT (empty to skip)')
  access_key=""; secret_key=""
  if [ -n "$endpoint" ]; then access_key=$(read_secret 'MINIO_ACCESS_KEY (empty to skip)'); fi
  if [ -n "$access_key" ]; then secret_key=$(read_secret 'MINIO_SECRET_KEY (empty to skip)'); fi
  if [ -n "$endpoint" ] && [ -n "$access_key" ] && [ -n "$secret_key" ]; then
    set_provider_keys MINIO_ENDPOINT "$endpoint" MINIO_ACCESS_KEY "$access_key" MINIO_SECRET_KEY "$secret_key"
  else
    MINIO=0
  fi
  unset access_key secret_key
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

mcp --resources-set "$SPEC" >/dev/null || {
  echo "[resources] profile save failed; supplied keys are in .env" >&2
  exit 1
}

echo "[resources] saved profile:"
mcp --resources
