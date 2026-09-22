#!/usr/bin/env sh
# Regression tests for scripts/smoke.sh transport selection.
#
#   --compose must use Docker even when a local bin/ binary exists
#   --binary  must use the local binary and never Docker
#   --auto    prefers the local binary
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' 0 INT TERM
FAIL=0

check() {
  name="$1"
  ok="$2"
  detail="$3"
  if [ "$ok" = "1" ]; then
    echo "[ok]   $name"
  else
    echo "[fail] $name${detail:+: $detail}"
    FAIL=$((FAIL + 1))
  fi
}

# Fake repository layout with a local binary and a fake docker CLI.
REPO="$TMP/repo"
mkdir -p "$REPO/scripts" "$REPO/bin" "$TMP/bin"
cp "$ROOT/scripts/smoke.sh" "$REPO/scripts/smoke.sh"
printf 'services: {}\n' > "$REPO/docker-compose.yml"

write_responder() {
  path="$1"
  marker="$2"
  cat > "$path" <<EOF
#!/usr/bin/env sh
count=\$(cat | wc -l | tr -d ' ')
printf '%s %s stdin=%s\n' "$marker" "\$*" "\$count" >> "\$MARKER"
printf '%s\n' \\
'{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}' \\
'{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}' \\
'{"jsonrpc":"2.0","id":3,"result":{"content":[],"isError":false}}'
EOF
  chmod +x "$path"
}

write_responder "$REPO/bin/hackathonkit-mcp" binary
write_responder "$TMP/bin/docker" docker

run_smoke() {
  HACKATHONKIT_SMOKE_DOCKER="$TMP/bin/docker" MARKER="$TMP/marker" PATH="$TMP/bin:/usr/bin:/bin" sh "$REPO/scripts/smoke.sh" "$@"
}

# --compose forces Docker even though bin/hackathonkit-mcp exists.
rm -f "$TMP/marker"
if run_smoke --compose >/dev/null 2>&1; then
  check "--compose exits 0" 1
else
  check "--compose exits 0" 0 "non-zero exit"
fi
check "--compose used docker" "$(grep -q '^docker .*stdin=3' "$TMP/marker" 2>/dev/null && echo 1 || echo 0)" "$(cat "$TMP/marker" 2>/dev/null)"
check "--compose skipped the local binary" "$(grep -q '^binary ' "$TMP/marker" 2>/dev/null && echo 0 || echo 1)"

# --binary uses the local binary and never Docker.
rm -f "$TMP/marker"
if run_smoke --binary >/dev/null 2>&1; then
  check "--binary exits 0" 1
else
  check "--binary exits 0" 0 "non-zero exit"
fi
check "--binary used the local binary" "$(grep -q '^binary .*stdin=3' "$TMP/marker" 2>/dev/null && echo 1 || echo 0)"
check "--binary skipped docker" "$(grep -q '^docker ' "$TMP/marker" 2>/dev/null && echo 0 || echo 1)"

# --auto prefers the local binary and must not touch docker.
rm -f "$TMP/marker"
if run_smoke --auto >/dev/null 2>&1; then
  check "--auto exits 0" 1
else
  check "--auto exits 0" 0 "non-zero exit"
fi
check "--auto prefers the local binary" "$(grep -q '^binary ' "$TMP/marker" 2>/dev/null && echo 1 || echo 0)"
check "--auto skips docker" "$(grep -q '^docker ' "$TMP/marker" 2>/dev/null && echo 0 || echo 1)"

# --compose without docker fails loudly.
rm -f "$TMP/marker"
if HACKATHONKIT_SMOKE_DOCKER="hackathonkit-no-such-docker" MARKER="$TMP/marker" PATH="$TMP/bin:/usr/bin:/bin" sh "$REPO/scripts/smoke.sh" --compose >/dev/null 2>&1; then
  COMPOSE_STATUS=0
else
  COMPOSE_STATUS=$?
fi
if [ "$COMPOSE_STATUS" = "1" ]; then
  check "--compose without docker fails with exit 1" 1
else
  check "--compose without docker fails with exit 1" 0 "exit=$COMPOSE_STATUS"
fi

# Unknown arguments are rejected.
if run_smoke --telepathy >/dev/null 2>&1; then
  UNKNOWN_STATUS=0
else
  UNKNOWN_STATUS=$?
fi
if [ "$UNKNOWN_STATUS" = "2" ]; then
  check "unknown argument rejected with exit 2" 1
else
  check "unknown argument rejected with exit 2" 0 "exit=$UNKNOWN_STATUS"
fi

if [ "$FAIL" -ne 0 ]; then
  echo "[smoke-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[smoke-test] all assertions passed"
