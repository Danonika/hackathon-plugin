#!/usr/bin/env sh
# Called from the repository root by setup and Docker verification.
set -eu
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$SCRIPT_DIR/file-lock.sh"
# Serialize first use so simultaneous setup and verification keep the same token.
acquire_file_lock .env.prepare-lock
if [ -L .env ] || { [ -e .env ] && [ ! -f .env ]; }; then
  echo "refusing non-regular .env" >&2
  exit 1
fi
if [ ! -f .env ]; then
  # Publish a fully written, private file without overwriting a concurrent creator.
  tmp=$(mktemp ./.env.tmp.XXXXXX)
  trap 'rm -f "$tmp"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  cat .env.example > "$tmp"
  if ! ln "$tmp" .env 2>/dev/null && { [ ! -f .env ] || [ -L .env ]; }; then
    echo "could not safely create .env" >&2
    exit 1
  fi
fi
chmod 600 .env

# Preserve an operator's configured token. The template and older templates
# used an empty value or a well-known placeholder.
if awk '
  /^[[:space:]]*(export[[:space:]]+)?HACKATHONKIT_DEMO_TOKEN[[:space:]]*=/ {
    value = $0
    sub(/^[^=]*=/, "", value)
    if (value ~ /^[[:space:]]+#/) { value = ""; next }
    sub(/^[[:space:]]+/, "", value)
    sub(/[[:space:]]+$/, "", value)
    quote = substr(value, 1, 1)
    if (quote == "\"" || quote == sprintf("%c", 39)) {
      value = substr(value, 2)
      sub(quote "[[:space:]]*(#.*)?$", "", value)
    } else {
      sub(/[[:space:]]+#.*/, "", value)
    }
  }
  END {
    exit !(length(value) && value != "change-me" && value != "change-me-in-production" &&
           value != "my-token" && value != "secret")
  }
' .env; then
  exit 0
fi

token=$(od -An -v -N16 -tx1 /dev/urandom | tr -d '[:space:]')
case "$token" in *[!0-9a-f]*) token="" ;; esac
if [ "${#token}" -ne 32 ]; then
  echo "could not generate a demo token; set HACKATHONKIT_DEMO_TOKEN in .env" >&2
  exit 1
fi
# The token travels through stdin and stays out of command arguments and logs.
printf '%s' "$token" | sh "$SCRIPT_DIR/env-set.sh" .env HACKATHONKIT_DEMO_TOKEN
