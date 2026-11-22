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
# Make the copied launcher executable so it is invoked via its shebang below,
# exercising the interpreter/exec-bit path rather than always `sh <file>`.
chmod +x "$REPO/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
printf 'module hackathonkit.local/agentkit\n' > "$REPO/go.mod"
printf 'services: {}\n' > "$REPO/docker-compose.yml"

cat > "$TMP/bin/docker" <<'EOF'
#!/usr/bin/env sh
printf 'docker %s\n' "$*" >> "$DOCKER_MARKER"
# Only `compose ... run` carries the status under test; every preflight
# subcommand (compose version, ...) must succeed regardless of DOCKER_STATUS.
case " $* " in
  *" run "*) exit "${DOCKER_STATUS:-0}" ;;
  *" version "*) exit "${DOCKER_VERSION_STATUS:-0}" ;;
  *) exit 1 ;;
esac
EOF
chmod +x "$TMP/bin/docker"

cat > "$REPO/bin/hackathonkit-mcp" <<'EOF'
#!/usr/bin/env sh
printf 'local %s\n' "$*" >> "$LOCAL_MARKER"
exit "${LOCAL_STATUS:-0}"
EOF
chmod 755 "$REPO/bin" "$REPO/bin/hackathonkit-mcp"

run_launcher() {
  PATH="$TMP/bin:$PATH" \
  LOCAL_MARKER="$TMP/local" \
  LOCAL_STATUS="${LOCAL_STATUS:-0}" \
  DOCKER_MARKER="$TMP/docker" \
  DOCKER_STATUS="${DOCKER_STATUS:-0}" DOCKER_VERSION_STATUS="${DOCKER_VERSION_STATUS:-0}" \
  "$REPO/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh" "$@"
}

# --- Local executable binary wins and receives the derived root -------------
: > "$TMP/local"
: > "$TMP/docker"
if ! run_launcher --list-tools; then check "local launcher invocation" 0; fi
# Assert the tokens independently instead of one order-sensitive substring.
if grep -Fq "local --repo-root $REPO" "$TMP/local" && grep -q -- "--list-tools" "$TMP/local"; then
  check "local binary is preferred with --repo-root and args" 1
else
  check "local binary is preferred with --repo-root and args" 0 "marker=$(cat "$TMP/local")"
fi
check "docker is not invoked when the local binary exists" "$( [ -s "$TMP/docker" ] && echo 0 || echo 1 )"

# A cached plugin must use the explicitly configured checkout for local runs.
CACHED="$TMP/cached plugin/plugins/hackathonkit/scripts"
mkdir -p "$CACHED"
cp "$LAUNCHER" "$CACHED/hackathonkit-mcp-launcher.sh"
: > "$TMP/local"
if HACKATHONKIT_REPO_ROOT="$REPO" LOCAL_MARKER="$TMP/local" sh "$CACHED/hackathonkit-mcp-launcher.sh" --list-tools &&
   grep -Fq "local --repo-root $REPO --list-tools" "$TMP/local"; then
  check "cached launcher honors the configured checkout" 1
else
  check "cached launcher honors the configured checkout" 0
fi

# --- Local exit status is propagated ----------------------------------------
chmod 755 "$REPO/bin" "$REPO/bin/hackathonkit-mcp"
if LOCAL_STATUS=5 run_launcher --list-tools; then
  local_status=0
else
  local_status=$?
fi
check "local exit status is propagated" "$( [ "$local_status" = "5" ] && echo 1 || echo 0 )" "status=$local_status"

# --- Non-executable local binary falls through to docker compose ------------
chmod -x "$REPO/bin/hackathonkit-mcp"
: > "$TMP/docker"
if ! run_launcher --list-tools; then check "docker launcher invocation" 0; fi
if grep -Fq "docker compose -f $REPO/docker-compose.yml run --rm -T hackathonkit-mcp --list-tools" "$TMP/docker"; then
  check "non-executable binary falls back to docker compose" 1
else
  check "non-executable binary falls back to docker compose" 0 "marker=$(cat "$TMP/docker")"
fi

# --- Docker exit status is propagated ---------------------------------------
# Set up the non-executable state explicitly so this does not depend on the
# previous block, and give DOCKER_STATUS a passthrough for the run subcommand.
chmod -x "$REPO/bin/hackathonkit-mcp"
: > "$TMP/docker"
if DOCKER_STATUS=7 run_launcher; then
  status=0
else
  status=$?
fi
check "docker exit status is propagated" "$( [ "$status" = "7" ] && echo 1 || echo 0 )" "status=$status"

# --- Caller-supplied --repo-root is rejected --------------------------------
chmod 755 "$REPO/bin" "$REPO/bin/hackathonkit-mcp"
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

# The `--repo-root=<value>` spelling is guarded too.
if run_launcher --repo-root=/tmp >/dev/null 2>"$TMP/err2"; then
  dup=0
else
  dup=$?
fi
if [ "$dup" -ne 0 ] && grep -q "must not be passed again" "$TMP/err2"; then
  check "duplicate --repo-root= is rejected" 1
else
  check "duplicate --repo-root= is rejected" 0 "status=$dup err=$(cat "$TMP/err2")"
fi

# The guard runs before the binary/docker branch, so it also applies when the
# local binary is non-executable (docker fallback path).
chmod -x "$REPO/bin/hackathonkit-mcp"
if run_launcher --repo-root=/tmp >/dev/null 2>"$TMP/err3"; then
  dup=0
else
  dup=$?
fi
if [ "$dup" -ne 0 ] && grep -q "must not be passed again" "$TMP/err3"; then
  check "duplicate --repo-root is rejected on the docker path" 1
else
  check "duplicate --repo-root is rejected on the docker path" 0 "status=$dup err=$(cat "$TMP/err3")"
fi
chmod 755 "$REPO/bin" "$REPO/bin/hackathonkit-mcp"

chmod -x "$REPO/bin/hackathonkit-mcp"
if DOCKER_VERSION_STATUS=3 run_launcher >/dev/null 2>"$TMP/err"; then
  check "a failed compose preflight stops the launcher" 0
else
  check "failed compose preflight is diagnosed" "$(grep -q 'compose (v2) is unavailable' "$TMP/err" && echo 1)"
fi

if [ "$FAIL" -gt 0 ]; then
  echo "[launcher-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[launcher-test] all assertions passed"
