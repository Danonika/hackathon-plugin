#!/usr/bin/env sh
# Run the Go test suite with coverage and enforce a minimum total.
# Threshold: HACKATHONKIT_COVERAGE_TARGET (default 80 percent).
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
TARGET="${HACKATHONKIT_COVERAGE_TARGET:-80}"
PROFILE="${TMPDIR:-/tmp}/hackathonkit-coverage.$$"
trap 'rm -f "$PROFILE"' EXIT INT TERM
cd "$ROOT"

# Containers may run as root over a repo owned by another user; disable VCS
# stamping so git's dubious-ownership check cannot fail the build.
case "${GOFLAGS:-}" in
  *-buildvcs*) ;;
  "") GOFLAGS="-buildvcs=false" ;;
  *) GOFLAGS="$GOFLAGS -buildvcs=false" ;;
esac
export GOFLAGS

if ! go test ./... -count=1 -coverprofile="$PROFILE" >/dev/null 2>&1; then
  echo "[coverage] test suite failed" >&2
  go test ./... -count=1 2>&1 | grep -E 'FAIL|--- FAIL' | head -20 >&2
  exit 1
fi

TOTAL=$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/, "", $3); print $3}')
echo "[coverage] total ${TOTAL}% (target ${TARGET}%)"

if ! awk -v total="$TOTAL" -v target="$TARGET" 'BEGIN { exit (total + 0 >= target + 0) ? 0 : 1 }'; then
  echo "[coverage] below target: ${TOTAL}% < ${TARGET}%" >&2
  go tool cover -func="$PROFILE" | sort -k3 -n | head -15 >&2
  exit 1
fi
