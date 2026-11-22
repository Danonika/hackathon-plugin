#!/usr/bin/env sh
# Called from the repository root by setup and Docker verification.
set -eu
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
