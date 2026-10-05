#!/usr/bin/env sh
# Run the build/verify gate and store evidence under evals/quality/<sha>/.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

# When running as root (e.g. the docker `quality` service), hand evidence written
# through the bind mount back to the host user so later non-root runs can write
# it. HACKATHONKIT_UID/GID come from the environment, or from .env (which the docker
# bind mount provides) when the service did not export them.
chown_evidence() {
  [ "$(id -u 2>/dev/null || echo 0)" = "0" ] || return 0
  owner="${HACKATHONKIT_UID:-}:${HACKATHONKIT_GID:-}"
  if [ "$owner" = ":" ] && [ -f "$ROOT/.env" ]; then
    uid=$(sed -n 's/^HACKATHONKIT_UID=\([0-9][0-9]*\).*/\1/p' "$ROOT/.env" | head -n 1)
    gid=$(sed -n 's/^HACKATHONKIT_GID=\([0-9][0-9]*\).*/\1/p' "$ROOT/.env" | head -n 1)
    owner="${uid:-}:${gid:-}"
  fi
  [ "$owner" != ":" ] || return 0
  [ -d "$ROOT/evals/quality" ] || return 0
  if ! chown -R "$owner" "$ROOT/evals/quality" 2>/dev/null; then
    echo "[quality-gate] warning: could not hand evals/quality back to $owner" >&2
  fi
}

BIN="$ROOT/bin/hackathonkit-mcp"
USE_BIN=0
if [ -x "$BIN" ]; then
  USE_BIN=1
  # A stale binary would report evidence for old sources: rebuild when the
  # sources are newer and a toolchain is available.
  if command -v go >/dev/null 2>&1; then
    NEWEST=""
    # Scan the whole module (not just cmd/ and internal/) so go.mod-only or
    # root-level changes are not missed; prune non-source trees for speed.
    # `find -print | head -n 1` is used instead of the non-POSIX `-quit`.
    NEWEST=$(find "$ROOT" \
        \( -path "$ROOT/.git" -o -path "$ROOT/node_modules" -o -path "$ROOT/data" -o -path "$ROOT/evals" -o -path "$ROOT/plugins" -o -path "$ROOT/bin" \) -prune -o \
        -name '*.go' -newer "$BIN" -print 2>/dev/null | head -n 1)
    for extra in "$ROOT/go.mod" "$ROOT/go.sum"; do
      if [ -f "$extra" ] && [ -n "$(find "$extra" -newer "$BIN" -print 2>/dev/null | head -n 1)" ]; then
        NEWEST="$extra"
        break
      fi
    done
    if [ -n "$NEWEST" ]; then
      echo "[quality-gate] bin/hackathonkit-mcp is older than $NEWEST; rebuilding" >&2
      USE_BIN=0
    fi
  else
    # A runnable binary is still usable without a toolchain (release tarballs,
    # go-less CI images), so run it and be explicit that freshness is unverified.
    echo "[quality-gate] warning: go toolchain unavailable; running the existing bin/hackathonkit-mcp without a freshness check" >&2
  fi
fi
if [ "$USE_BIN" = "1" ]; then
  status=0
  "$BIN" --repo-root "$ROOT" --quality-gate || status=$?
  chown_evidence
  exit "$status"
fi

if command -v go >/dev/null 2>&1; then
  TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/hackathonkit-mcp-quality.XXXXXX")
  # Resolve to an absolute path before cd so a relative TMPDIR still works.
  TMP_DIR="$(CDPATH= cd -- "$TMP_DIR" && pwd)"
  TMP_BIN="$TMP_DIR/hackathonkit-mcp"
  cleanup_tmp() { rm -rf "$TMP_DIR"; chown_evidence; }
  trap cleanup_tmp EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  cd "$ROOT"
  go build -buildvcs=false -o "$TMP_BIN" ./cmd/hackathonkit-mcp || {
    echo "[quality-gate] go build failed" >&2
    exit 1
  }
  # No exec: the EXIT trap must run to remove the temporary binary. `|| status=$?`
  # keeps `set -e` from skipping the chown and the exit code.
  status=0
  "$TMP_BIN" --repo-root "$ROOT" --quality-gate || status=$?
  chown_evidence
  exit "$status"
fi

echo "[quality-gate] no runtime available; build one with scripts/setup.sh" >&2
exit 2
