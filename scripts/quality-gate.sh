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
  case "$owner" in
    *[!0-9:]*|:*|*:|*:*:*) return 0 ;;
  esac
  [ -d "$ROOT/evals/quality" ] || return 0
  revision=$(git -C "$ROOT" rev-parse --verify HEAD 2>/dev/null || echo unknown)
  case "$revision" in unknown) ;; *[!0-9a-f]*|'') return 0 ;; esac
  evidence="$ROOT/evals/quality/$revision"
  if [ -L "$ROOT/evals" ] || [ -L "$ROOT/evals/quality" ] || [ -L "$evidence" ]; then return 1; fi
  [ -f "$evidence/gate.json" ] && [ ! -L "$evidence/gate.json" ] || return 0
  if ! chown "$owner" "$ROOT/evals/quality" "$evidence" "$evidence/gate.json" 2>/dev/null; then
    echo "[quality-gate] warning: could not hand evals/quality back to $owner" >&2
  fi
}

BIN="$ROOT/bin/hackathonkit-mcp"
# Use Go's dependency-aware cache whenever a toolchain is available. A release
# installation with no Go can still verify its existing native runtime.
if ! command -v go >/dev/null 2>&1 && [ "${HACKATHONKIT_QUALITY_FROM_SOURCE:-0}" != 1 ] && [ -x "$BIN" ]; then
  echo "[quality-gate] Go unavailable; using the installed gate driver" >&2
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
  cleanup_tmp() { rm -rf "$TMP_DIR"; }
  trap cleanup_tmp EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  cd "$ROOT"
  go build -trimpath -buildvcs=false -o "$TMP_BIN" ./cmd/hackathonkit-mcp || {
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
