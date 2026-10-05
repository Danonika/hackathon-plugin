#!/usr/bin/env sh
# Regression tests for plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh:
# local-binary preference, --repo-root injection, argument passthrough, the
# non-executable fallback to docker compose, exit-status propagation and the
# duplicate --repo-root guard.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/../../.." && pwd)"
LAUNCHER="$ROOT/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
TMP="$(mktemp -d)"
cleanup() { rm -rf "$TMP"; }
trap cleanup 0
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM
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

REPO="$TMP/repo"
mkdir -p "$REPO/plugins/hackathonkit/scripts" "$REPO/bin" "$TMP/bin"
cp "$LAUNCHER" "$REPO/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
printf 'module example.test/repo\n' > "$REPO/go.mod"
printf 'services: {}\n' > "$REPO/docker-compose.yml"

cat > "$TMP/bin/docker" <<'EOF'
#!/usr/bin/env sh
printf 'docker %s\n' "$*" >> "$DOCKER_MARKER"
if [ "$1" = "compose" ] && [ "$2" = "version" ]; then
  exit 0
fi
exit "${DOCKER_STATUS:-0}"
EOF
chmod +x "$TMP/bin/docker"

cat > "$REPO/bin/hackathonkit-mcp" <<'EOF'
#!/usr/bin/env sh
printf 'local %s\n' "$*" >> "$LOCAL_MARKER"
exit "${LOCAL_STATUS:-0}"
EOF
chmod +x "$REPO/bin/hackathonkit-mcp"

run_launcher() {
  PATH="$TMP/bin:$PATH" \
  LOCAL_MARKER="$TMP/local" \
  DOCKER_MARKER="$TMP/docker" \
  DOCKER_STATUS="${DOCKER_STATUS:-0}" \
  sh "$REPO/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh" "$@"
}

# --- Local executable binary wins and receives the derived root -------------
: > "$TMP/local"
: > "$TMP/docker"
run_launcher --list-tools
if grep -q "local --repo-root $REPO --list-tools" "$TMP/local"; then
  check "local binary is preferred with --repo-root and args" 1
else
  check "local binary is preferred with --repo-root and args" 0 "marker=$(cat "$TMP/local")"
fi
check "docker is not invoked when the local binary exists" "$( [ -s "$TMP/docker" ] && echo 0 || echo 1 )"

# --- Non-executable local binary falls through to docker compose ------------
chmod -x "$REPO/bin/hackathonkit-mcp"
: > "$TMP/docker"
run_launcher --list-tools
if grep -q "docker compose -f $REPO/docker-compose.yml run --rm -T hackathonkit-mcp --list-tools" "$TMP/docker"; then
  check "non-executable binary falls back to docker compose" 1
else
  check "non-executable binary falls back to docker compose" 0 "marker=$(cat "$TMP/docker")"
fi

# --- Docker exit status is propagated ---------------------------------------
: > "$TMP/docker"
if DOCKER_STATUS=7 run_launcher; then
  status=0
else
  status=$?
fi
check "docker exit status is propagated" "$( [ "$status" = "7" ] && echo 1 || echo 0 )" "status=$status"

# --- Caller-supplied --repo-root is rejected --------------------------------
chmod +x "$REPO/bin/hackathonkit-mcp"
if run_launcher --repo-root /tmp >/dev/null 2>"$TMP/err"; then
  dup=0
else
  dup=$?
fi
if [ "$dup" -ne 0 ] && grep -q "must not be passed again" "$TMP/err"; then
  check "duplicate --repo-root is rejected" 1
else
  check "duplicate --repo-root is rejected" 0 "status=$dup err=$(cat "$TMP/err")"
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[launcher-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[launcher-test] all assertions passed"
