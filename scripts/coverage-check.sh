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
trap 'rm -f "$PROFILE"' EXIT INT TERM
cd "$ROOT"

# Containers may run as root over a repo owned by another user; disable VCS
# stamping so git's dubious-ownership check cannot fail the build.
GOFLAGS=$(printf '%s' "${GOFLAGS:-}" | sed -E 's/[[:space:]]*-buildvcs=[^[:space:]]*//g; s/^[[:space:]]+//; s/[[:space:]]+$//')
if [ -n "$GOFLAGS" ]; then
  GOFLAGS="$GOFLAGS -buildvcs=false"
else
  GOFLAGS="-buildvcs=false"
fi
export GOFLAGS

if ! TEST_OUT=$(go test ./... -count=1 -coverprofile="$PROFILE" 2>&1); then
  echo "[coverage] test suite failed" >&2
  printf '%s\n' "$TEST_OUT" | tail -30 >&2
  exit 1
fi

TOTAL=$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')
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
