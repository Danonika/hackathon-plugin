#!/usr/bin/env sh
# Run the Go test suite with coverage and enforce a minimum total.
# Threshold: HACKATHONKIT_COVERAGE_TARGET (default 80 percent).
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TARGET="${HACKATHONKIT_COVERAGE_TARGET:-80}"
case "$TARGET" in
  ''|*[!0-9.]*)
    echo "[coverage] HACKATHONKIT_COVERAGE_TARGET must be a number, got '$TARGET'" >&2
    exit 2
    ;;
esac
case "$TARGET" in
  *[0-9]*) ;;
  *) echo "[coverage] HACKATHONKIT_COVERAGE_TARGET must contain a digit, got '$TARGET'" >&2; exit 2 ;;
esac
if [ "$(printf '%s' "$TARGET" | tr -cd '.' | wc -c | tr -d ' ')" -gt 1 ]; then
  echo "[coverage] HACKATHONKIT_COVERAGE_TARGET must have at most one dot, got '$TARGET'" >&2
  exit 2
fi
if ! awk -v t="$TARGET" 'BEGIN { exit (t + 0 >= 0 && t + 0 <= 100) ? 0 : 1 }'; then
  echo "[coverage] HACKATHONKIT_COVERAGE_TARGET must be between 0 and 100, got '$TARGET'" >&2
  exit 2
fi
PROFILE=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-coverage.XXXXXX")
cleanup_profile() { rm -f "$PROFILE"; }
trap cleanup_profile EXIT
trap 'cleanup_profile; exit 130' INT
trap 'cleanup_profile; exit 143' TERM
cd "$ROOT"

# Containers may run as root over a repo owned by another user; disable VCS
# stamping so git's dubious-ownership check cannot fail the build.
GOFLAGS=$(printf '%s' "${GOFLAGS:-}" | sed -E 's/(^|[[:space:]])-buildvcs=[^[:space:]]*/\1/g; s/^[[:space:]]+//; s/[[:space:]]+$//')
if [ -n "$GOFLAGS" ]; then
  GOFLAGS="$GOFLAGS -buildvcs=false"
else
  GOFLAGS="-buildvcs=false"
fi
export GOFLAGS

TEST_LOG=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-coverage-test.XXXXXX")
if ! go test ./... -count=1 -coverprofile="$PROFILE" >"$TEST_LOG" 2>&1; then
  echo "[coverage] test suite failed" >&2
  tail -30 "$TEST_LOG" >&2
  rm -f "$TEST_LOG"
  exit 1
fi
rm -f "$TEST_LOG"

COVER_LOG=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-coverage-func.XXXXXX")
if ! go tool cover -func="$PROFILE" >"$COVER_LOG" 2>&1; then
  echo "[coverage] go tool cover failed" >&2
  tail -5 "$COVER_LOG" >&2
  rm -f "$COVER_LOG"
  exit 1
fi
TOTAL=$(awk '/^total:/ {gsub(/%/, "", $3); print $3}' "$COVER_LOG")
rm -f "$COVER_LOG"
if [ -z "$TOTAL" ]; then
  echo "[coverage] could not read a total from the coverage profile" >&2
  exit 1
fi
case "$TOTAL" in
  ''|*[!0-9.]*)
    echo "[coverage] malformed coverage total: '$TOTAL'" >&2
    exit 1
    ;;
esac
echo "[coverage] total ${TOTAL}% (target ${TARGET}%)"

if ! awk -v total="$TOTAL" -v target="$TARGET" 'BEGIN { exit (total + 0 >= target + 0) ? 0 : 1 }'; then
  echo "[coverage] below target: ${TOTAL}% < ${TARGET}%" >&2
  go tool cover -func="$PROFILE" | sort -k3 -n | head -15 >&2
  exit 1
fi
