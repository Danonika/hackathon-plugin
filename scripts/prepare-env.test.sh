#!/usr/bin/env sh
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
cd "$WORK"
cp "$ROOT/.env.example" .env.example

check_generated_token() {
  awk -F= '
    $1 == "HACKATHONKIT_DEMO_TOKEN" {
      found++
      valid = length($2) == 32 && $2 ~ /^[0-9a-f]+$/
    }
    END { exit !(found == 1 && valid) }
  ' .env || { echo "fresh checkout has no generated demo token" >&2; exit 1; }
}

sh "$ROOT/scripts/prepare-env.sh" >setup.log 2>&1
check_generated_token
mode=$(stat -c '%a' .env 2>/dev/null || stat -f '%Lp' .env)
[ "$mode" = 600 ]
# Verify without printing either the token or the rendered Compose config.
if awk -F= '$1 == "HACKATHONKIT_DEMO_TOKEN" { print $2 }' .env | grep -Fq -f - setup.log; then
  echo "preparation output contains the generated token" >&2
  exit 1
fi
if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
  docker compose --env-file .env -f "$ROOT/docker-compose.yml" config --quiet
fi
cp .env previous
sh "$ROOT/scripts/prepare-env.sh" >/dev/null
cmp -s previous .env

printf '%s\n' 'HACKATHONKIT_DEMO_TOKEN=" #configured-by-operator" # keep this setting' 'EXTRA_SETTING=keep' > .env
cp .env previous
sh "$ROOT/scripts/prepare-env.sh" >/dev/null
cmp -s previous .env

printf '%s\n' 'HACKATHONKIT_DEMO_TOKEN=change-me' > .env
sh "$ROOT/scripts/prepare-env.sh" >/dev/null
check_generated_token
echo '[prepare-env-test] private fresh token, stable rerun, existing token and placeholder checks passed'
