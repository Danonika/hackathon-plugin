#!/usr/bin/env sh
# Run a *deterministic* OCR scan and merge the result into the canonical
# backlog, so the "open unique findings" count is monotonic instead of churning
# with each non-reproducible full scan.
#
# Why the flags: PLAN/DEDUP/SUMMARY subtasks and parallel batching are the main
# sources of run-to-run variance. Disabling them and forcing concurrency 1
# raises repeat-scan overlap from ~40% to ~80% (measured on internal/tools/data.go).
#
# Usage:
#   scripts/ocr-baseline.sh                      # whole repo
#   scripts/ocr-baseline.sh --path internal/tools # one path
#   scripts/ocr-baseline.sh --set fixed --match <sig>   # triage a finding
#
# The canonical backlog lives at docs/ocr-baseline.json; a readable summary is
# written to docs/OCR-CANONICAL.md.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
BASELINE="$ROOT/docs/ocr-baseline.json"
OUT_MD="$ROOT/docs/OCR-CANONICAL.md"

if ! command -v python3 >/dev/null 2>&1; then
  echo "[ocr-baseline] python3 is required" >&2
  exit 2
fi

# Triage mode: apply a status to an existing finding instead of scanning.
if [ "${1:-}" = "--set" ]; then
  shift
  exec python3 "$ROOT/scripts/ocr-dedup.py" --baseline "$BASELINE" --out-json "$BASELINE" --out-md "$OUT_MD" --set "$@"
fi

if ! command -v ocr >/dev/null 2>&1; then
  echo "[ocr-baseline] ocr CLI not found on PATH" >&2
  exit 2
fi
cd "$ROOT"
SCAN=$(mktemp "${TMPDIR:-/tmp}/ocr-scan.XXXXXX")
trap 'rm -f "$SCAN"' EXIT INT TERM

# Deterministic scan: no plan/dedup/summary subtasks, single concurrency.
ocr scan --no-plan --no-dedup --no-summary --concurrency 1 \
  --format json -o "$SCAN" --audience agent "$@"

python3 "$ROOT/scripts/ocr-dedup.py" \
  --baseline "$BASELINE" \
  --out-json "$BASELINE" \
  --out-md "$OUT_MD" \
  --summary "$SCAN"
