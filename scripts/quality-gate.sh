#!/usr/bin/env sh
# Run the build/verify gate and store evidence under evals/quality/<sha>/.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  exec "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --quality-gate
fi

if command -v go >/dev/null 2>&1; then
  TMP_BIN=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-mcp-quality.XXXXXX")
  trap 'rm -f "$TMP_BIN"' EXIT INT TERM
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
