#!/usr/bin/env sh
# Regression tests for scripts/smoke.sh transport selection.
#
#   --compose must use Docker even when a local bin/ binary exists
#   --binary  must use the local binary and never Docker
#   --auto    prefers the local binary
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TMP="$(mktemp -d)"
cleanup_smoke_test() { rm -rf "$TMP"; }
trap cleanup_smoke_test 0
trap 'cleanup_smoke_test; exit 130' INT
trap 'cleanup_smoke_test; exit 143' TERM
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
  # Quoted heredoc: every '$' in the body reaches the stub verbatim, so editing
  # the JSON payload can never silently corrupt it. The label is substituted
  # afterwards with a portable sed round-trip.
  cat > "$path" <<'EOF'
#!/usr/bin/env sh
count=$(cat | wc -l | tr -d ' ')
printf '%s %s stdin=%s\n' "@MARKER@" "$*" "$count" >> "$MARKER"
printf '%s\n' \
'{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18"}}' \
'{"jsonrpc":"2.0","id":2,"result":{"tools":[]}}' \
'{"jsonrpc":"2.0","id":3,"result":{"content":[],"isError":false}}'
EOF
  # Escape replacement metacharacters (backslash, slash, ampersand) so a marker
  # containing them cannot corrupt the generated stub.
  escaped_marker=$(printf '%s' "$marker" | sed -e 's/\\/\\\\/g' -e 's/\//\\\//g' -e 's/&/\\&/g')
  sed "s/@MARKER@/$escaped_marker/" "$path" > "$path.new" && mv "$path.new" "$path"
  chmod +x "$path"
}

# Derive the expected request count from smoke.sh instead of hard-coding it.
# `grep -c` exits 1 on no match and would abort the whole suite under `set -e`,
# so derive it non-fatally and assert it explicitly.
EXPECTED_STDIN=$(grep -c '^{"jsonrpc":"2.0","id":' "$ROOT/scripts/smoke.sh" 2>/dev/null || true)
if [ -z "$EXPECTED_STDIN" ] || [ "$EXPECTED_STDIN" -lt 1 ]; then
  echo "[smoke-test] could not derive the request count from scripts/smoke.sh" >&2
  exit 1
fi

write_responder "$REPO/bin/hackathonkit-mcp" binary
write_responder "$TMP/bin/docker" docker

run_smoke() {
  HACKATHONKIT_SMOKE_DOCKER="$TMP/bin/docker" MARKER="$TMP/marker" PATH="$TMP/bin:$PATH" sh "$REPO/scripts/smoke.sh" "$@"
}

# --compose forces Docker even though bin/hackathonkit-mcp exists.
rm -f "$TMP/marker"
if run_smoke --compose >/dev/null 2>&1; then
  check "--compose exits 0" 1
else
  check "--compose exits 0" 0 "non-zero exit"
fi
check "--compose used docker with the compose args" "$(grep -q "run --rm -T hackathonkit-mcp stdin=$EXPECTED_STDIN" "$TMP/marker" 2>/dev/null && echo 1 || echo 0)" "$(cat "$TMP/marker" 2>/dev/null)"
check "--compose skipped the local binary" "$(grep -q '^binary ' "$TMP/marker" 2>/dev/null && echo 0 || echo 1)"

# --binary uses the local binary and never Docker.
rm -f "$TMP/marker"
if run_smoke --binary >/dev/null 2>&1; then
  check "--binary exits 0" 1
else
  check "--binary exits 0" 0 "non-zero exit"
fi
check "--binary used the local binary with --repo-root" "$(grep -q "^binary --repo-root .*stdin=$EXPECTED_STDIN" "$TMP/marker" 2>/dev/null && echo 1 || echo 0)"
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

# --auto detects a stale local binary (a newer .go source) and uses compose.
mkdir -p "$REPO/cmd" "$REPO/internal"
# Force a guaranteed ordering regardless of filesystem timestamp granularity:
# backdate the binary so the freshly written source is unambiguously newer.
touch -t 200001010000 "$REPO/bin/hackathonkit-mcp"
printf 'package main\n' > "$REPO/cmd/main.go"
rm -f "$TMP/marker"
if run_smoke --auto >/dev/null 2>&1; then
  check "--auto stale binary exits 0" 1
else
  check "--auto stale binary exits 0" 0 "non-zero exit"
fi
check "--auto uses compose when the local binary is stale" "$(grep -q '^docker compose ' "$TMP/marker" 2>/dev/null && echo 1 || echo 0)" "$(cat "$TMP/marker" 2>/dev/null)"

# --auto without a local binary falls back to Docker.
rm -f "$TMP/marker" "$REPO/bin/hackathonkit-mcp"
if run_smoke --auto >/dev/null 2>&1; then
  check "--auto falls back to docker" 1
else
  check "--auto falls back to docker" 0 "non-zero exit"
fi
check "--auto used docker when the binary is absent" "$(grep -q '^docker compose ' "$TMP/marker" 2>/dev/null && echo 1 || echo 0)" "$(cat "$TMP/marker" 2>/dev/null)"

# --auto with neither a local binary nor the docker CLI exits 1.
rm -f "$TMP/marker"
if HACKATHONKIT_SMOKE_DOCKER="hackathonkit-no-such-docker" MARKER="$TMP/marker" PATH="/usr/bin:/bin" sh "$REPO/scripts/smoke.sh" --auto >/dev/null 2>&1; then
  check "--auto without any runtime fails with exit 1" 0 "expected non-zero exit"
else
  check "--auto without any runtime fails with exit 1" 1
fi

# --binary without a local binary fails with a clear status.
rm -f "$TMP/marker"
if run_smoke --binary >/dev/null 2>&1; then
  BINARY_STATUS=0
else
  BINARY_STATUS=$?
fi
if [ "$BINARY_STATUS" = "1" ]; then
  check "--binary without a binary fails with exit 1" 1
else
  check "--binary without a binary fails with exit 1" 0 "exit=$BINARY_STATUS"
fi
write_responder "$REPO/bin/hackathonkit-mcp" binary

# --compose without docker fails loudly.
rm -f "$TMP/marker"
if HACKATHONKIT_SMOKE_DOCKER="hackathonkit-no-such-docker" MARKER="$TMP/marker" PATH="/usr/bin:/bin" sh "$REPO/scripts/smoke.sh" --compose >/dev/null 2>&1; then
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
