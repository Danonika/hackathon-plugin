#!/usr/bin/env sh
# Regression tests for scripts/env-set.sh: .env permission preservation.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
HELPER="$ROOT/scripts/env-set.sh"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT INT TERM
FAIL=0

check() {
  name="$1"
  ok="$2"
  detail="$3"
  if [ "$ok" = "1" ]; then
    echo "[ok]   $name"
  else
    echo "[fail] $name${detail:+: $detail}"
    FAIL=$((FAIL + 1))
  fi
}

file_mode() {
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null || echo unknown
}

# An existing private .env must not be downgraded by the rewrite (umask 022).
(umask 022 && printf 'A=1\n' > "$TMP/private.env" && chmod 600 "$TMP/private.env")
(umask 022 && sh "$HELPER" "$TMP/private.env" A 2)
check "existing 0600 .env keeps its mode" "$([ "$(file_mode "$TMP/private.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/private.env")"
check "existing key is replaced" "$(grep -q '^A=2$' "$TMP/private.env" && echo 1 || echo 0)"
check "no stray keys" "$([ "$(grep -c '^' "$TMP/private.env")" = "1" ] && echo 1 || echo 0)"

# A new file is created private even under umask 022.
(umask 022 && sh "$HELPER" "$TMP/new.env" KEY value)
check "new .env is created 0600" "$([ "$(file_mode "$TMP/new.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/new.env")"
check "new key is appended" "$(grep -q '^KEY=value$' "$TMP/new.env" && echo 1 || echo 0)"

# A non-private existing file keeps its own mode.
(umask 022 && printf 'B=1\n' > "$TMP/plain.env" && chmod 644 "$TMP/plain.env")
(umask 022 && sh "$HELPER" "$TMP/plain.env" B 2)
check "existing 0644 .env keeps its mode" "$([ "$(file_mode "$TMP/plain.env")" = "644" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/plain.env")"

# Missing arguments are rejected.
if sh "$HELPER" >/dev/null 2>&1; then
  check "missing arguments rejected" 0 "expected a non-zero exit"
else
  check "missing arguments rejected" 1
fi

if [ "$FAIL" -ne 0 ]; then
  echo "[env-set-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[env-set-test] all assertions passed"
