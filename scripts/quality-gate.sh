#!/usr/bin/env sh
# Run the build/verify gate and store evidence under evals/quality/<sha>/.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  exec "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --quality-gate
fi

if command -v go >/dev/null 2>&1; then
  TMP_BIN="${TMPDIR:-/tmp}/hackathonkit-mcp-quality.$$"
  trap 'rm -f "$TMP_BIN"' EXIT INT TERM
  cd "$ROOT"
  go build -buildvcs=false -o "$TMP_BIN" ./cmd/hackathonkit-mcp
  exec "$TMP_BIN" --repo-root "$ROOT" --quality-gate
fi

echo "[quality-gate] no runtime available; build one with scripts/setup.sh" >&2
exit 2
