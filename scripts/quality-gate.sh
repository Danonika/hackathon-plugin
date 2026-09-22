#!/usr/bin/env sh
# Run the build/verify gate and store evidence under evals/quality/<sha>/.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

BIN="$ROOT/bin/hackathonkit-mcp"
USE_BIN=0
if [ -x "$BIN" ]; then
  USE_BIN=1
  # A stale binary would report evidence for old sources: rebuild when the
  # sources are newer and a toolchain is available.
  if command -v go >/dev/null 2>&1; then
    NEWEST=$(find "$ROOT/cmd" "$ROOT/internal" -name '*.go' -newer "$BIN" 2>/dev/null | head -n 1)
    for extra in "$ROOT/go.mod" "$ROOT/go.sum"; do
      if [ -f "$extra" ] && [ "$extra" -nt "$BIN" ]; then
        NEWEST="$extra"
        break
      fi
    done
    if [ -n "$NEWEST" ]; then
      echo "[quality-gate] bin/hackathonkit-mcp is older than $NEWEST; rebuilding" >&2
      USE_BIN=0
    fi
  fi
fi
if [ "$USE_BIN" = "1" ]; then
  exec "$BIN" --repo-root "$ROOT" --quality-gate
fi

if command -v go >/dev/null 2>&1; then
  TMP_BIN=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-mcp-quality.XXXXXX")
  # Resolve to an absolute path before cd so a relative TMPDIR still works.
  TMP_BIN="$(CDPATH= cd -- "$(dirname -- "$TMP_BIN")" && pwd)/$(basename -- "$TMP_BIN")"
  cleanup_tmp() { rm -f "$TMP_BIN"; }
  trap cleanup_tmp EXIT
  trap 'cleanup_tmp; exit 130' INT
  trap 'cleanup_tmp; exit 143' TERM
  cd "$ROOT"
  go build -buildvcs=false -o "$TMP_BIN" ./cmd/hackathonkit-mcp || {
    echo "[quality-gate] go build failed" >&2
    exit 1
  }
  # No exec: the EXIT trap must run to remove the temporary binary.
  "$TMP_BIN" --repo-root "$ROOT" --quality-gate || exit $?
  exit 0
fi

echo "[quality-gate] no runtime available; build one with scripts/setup.sh" >&2
exit 2
