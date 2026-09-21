#!/usr/bin/env sh
# Validate the skill catalog against docs/SKILL-STANDARD.md.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  exec "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --validate-skills
fi

if command -v go >/dev/null 2>&1; then
  cd "$ROOT"
  exec go run ./cmd/hackathonkit-mcp --repo-root "$ROOT" --validate-skills
fi

echo "[validate-skills] no runtime available; build one with scripts/setup.sh" >&2
exit 2
