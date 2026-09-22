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
  cleanup_tmp() { rm -f "$TMP_BIN"; }
  on_signal() { cleanup_tmp; exit 130; }
  trap cleanup_tmp EXIT
  trap on_signal INT TERM
  cd "$ROOT"
  go build -buildvcs=false -o "$TMP_BIN" ./cmd/hackathonkit-mcp || {
    echo "[quality-gate] go build failed" >&2
    exit 1
  }
  # No exec: the EXIT trap must run to remove the temporary binary.
  "$TMP_BIN" --repo-root "$ROOT" --quality-gate
  exit $?
fi

echo "[quality-gate] no runtime available; build one with scripts/setup.sh" >&2
exit 2
