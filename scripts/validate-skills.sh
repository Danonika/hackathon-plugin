#!/usr/bin/env sh
# Validate the skill catalog against docs/SKILL-STANDARD.md.
set -e
SELF="$0"
case "$SELF" in
  */*) ;;
  *)
    # Invoked via PATH: resolve the real path. If that fails, fail clearly
    # instead of letting `dirname` return "." and pick the wrong root.
    RESOLVED=$(command -v "$SELF" 2>/dev/null || true)
    if [ -z "$RESOLVED" ]; then
      echo "[validate-skills] cannot resolve the script path from '$SELF'; run it by path" >&2
      exit 1
    fi
    SELF="$RESOLVED"
    ;;
esac
ROOT="$(CDPATH= cd -- "$(dirname "$SELF")/.." && pwd)" || {
  echo "[validate-skills] cannot resolve the repository root" >&2
  exit 1
}
if [ ! -f "$ROOT/go.mod" ] || [ ! -d "$ROOT/plugins/hackathonkit/skills" ] || [ ! -f "$ROOT/docs/SKILL-STANDARD.md" ]; then
  echo "[validate-skills] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi

if [ -f "$ROOT/bin/hackathonkit-mcp" ] && [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  status=0
  "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --validate-skills || status=$?
  case "$status" in
    126|127) echo "[validate-skills] cannot execute local binary; trying Go" >&2 ;;
    *) exit "$status" ;;
  esac
fi

if command -v go >/dev/null 2>&1; then
  cd "$ROOT" || { echo "[validate-skills] cannot enter $ROOT" >&2; exit 1; }
  if go run ./cmd/hackathonkit-mcp --repo-root "$ROOT" --validate-skills; then
    exit 0
  else
    status=$?
    # `go run` exits non-zero for toolchain/build/module errors too, not only
    # catalog violations, so report the captured status instead of masking it.
    echo "[validate-skills] go run exited $status (validation failure or toolchain/build error)" >&2
    exit "$status"
  fi
fi

echo "[validate-skills] no runtime available; build one with scripts/setup.sh" >&2
exit 2
