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
  stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1" 2>/dev/null || {
    echo "[env-set-test] no supported stat variant" >&2
    exit 2
  }
}

# An existing private .env must not be downgraded by the rewrite (umask 022).
(umask 022 && printf 'A=1\n' > "$TMP/private.env" && chmod 600 "$TMP/private.env")
(umask 022 && sh "$HELPER" "$TMP/private.env" A 2) || {
  echo "[env-set-test] helper failed for private.env" >&2
  exit 1
}
check "existing 0600 .env keeps its mode" "$([ "$(file_mode "$TMP/private.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/private.env")"
check "existing key is replaced" "$(grep -q '^A=2$' "$TMP/private.env" && echo 1 || echo 0)" "got: $(cat "$TMP/private.env")"
check "no stray keys" "$([ "$(grep -c '^' "$TMP/private.env")" = "1" ] && echo 1 || echo 0)"

# A new file is created private even under umask 022.
(umask 022 && sh "$HELPER" "$TMP/new.env" KEY value) || {
  echo "[env-set-test] helper failed for new.env" >&2
  exit 1
}
check "new .env is created 0600" "$([ "$(file_mode "$TMP/new.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/new.env")"
check "new key is appended" "$(grep -q '^KEY=value$' "$TMP/new.env" && echo 1 || echo 0)" "got: $(cat "$TMP/new.env")"

# A non-private existing file keeps its own mode.
(umask 022 && printf 'B=1\n' > "$TMP/plain.env" && chmod 644 "$TMP/plain.env")
(umask 022 && sh "$HELPER" "$TMP/plain.env" B 2) || {
  echo "[env-set-test] helper failed for plain.env" >&2
  exit 1
}
check "existing 0644 .env keeps its mode" "$([ "$(file_mode "$TMP/plain.env")" = "644" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/plain.env")"

# Missing arguments are rejected.
if sh "$HELPER" >/dev/null 2>&1; then
  check "missing arguments rejected" 0 "expected a non-zero exit"
else
  check "missing arguments rejected" 1
fi

# Appending to a non-empty file keeps unrelated keys intact.
printf 'KEEP=1\n' > "$TMP/append.env"
sh "$HELPER" "$TMP/append.env" NEW value
check "append keeps existing keys" "$(grep -q '^KEEP=1$' "$TMP/append.env" && echo 1 || echo 0)"
check "append adds the new key" "$(grep -q '^NEW=value$' "$TMP/append.env" && echo 1 || echo 0)"

# The value can be fed through stdin so secrets stay out of argv.
(umask 022 && printf 'from-stdin' | sh "$HELPER" "$TMP/stdin.env" S)
check "stdin value is stored" "$(grep -q '^S=from-stdin$' "$TMP/stdin.env" && echo 1 || echo 0)"
check "stdin value file is 0600" "$([ "$(file_mode "$TMP/stdin.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/stdin.env")"

# A single trailing newline from stdin is a terminator, not part of the value.
(umask 022 && printf 'trailing\n' | sh "$HELPER" "$TMP/stdin-nl.env" NL)
check "stdin trailing newline is stripped" "$(grep -q '^NL=trailing$' "$TMP/stdin-nl.env" && echo 1 || echo 0)" "got: $(cat "$TMP/stdin-nl.env")"

# A value containing spaces/quotes is quoted so the .env stays parseable.
sh "$HELPER" "$TMP/quote.env" SPACED 'two words'
check "spaced value is quoted" "$(grep -q '^SPACED="two words"$' "$TMP/quote.env" && echo 1 || echo 0)" "got: $(cat "$TMP/quote.env")"

# An embedded newline is preserved as an escaped \n inside a quoted value.
(umask 022 && printf 'line1\nline2\n' | sh "$HELPER" "$TMP/multi.env" MULTI)
check "embedded newline is escaped" "$(grep -q '^MULTI="line1\\nline2"$' "$TMP/multi.env" && echo 1 || echo 0)" "got: $(cat "$TMP/multi.env")"

# Backslashes trigger quoting and are escaped so a sourced .env yields the
# original value ("p\q\r").
sh "$HELPER" "$TMP/escape.env" B 'p\q\r'
check "backslash value is escaped" "$(grep -q '^B="p\\\\q\\\\r"$' "$TMP/escape.env" && echo 1 || echo 0)" "got: $(cat "$TMP/escape.env")"

# Command-substitution and backtick payloads must be emitted inert: sourcing the
# file must not execute them.
sh "$HELPER" "$TMP/inject.env" EVIL '$(touch /tmp/hackathonkit-env-inject-marker)'
rm -f /tmp/hackathonkit-env-inject-marker
( set -a; . "$TMP/inject.env"; set +a; printf '%s\n' "$EVIL" > "$TMP/evil-value" )
check "dollar substitution is not executed" "$([ -f /tmp/hackathonkit-env-inject-marker ] && echo 0 || echo 1)" "got: $(cat "$TMP/inject.env")"
check "dollar value round-trips" "$([ "$(cat "$TMP/evil-value")" = '$(touch /tmp/hackathonkit-env-inject-marker)' ] && echo 1 || echo 0)" "got: $(cat "$TMP/evil-value")"
rm -f /tmp/hackathonkit-env-inject-marker

# Keys are matched literally, not as regexes.
printf 'A_B=old\nA_XB=keep\n' > "$TMP/literal.env"
sh "$HELPER" "$TMP/literal.env" A_B new
check "literal key is replaced" "$(grep -q '^A_B=new$' "$TMP/literal.env" && echo 1 || echo 0)"
check "lookalike key is untouched" "$(grep -q '^A_XB=keep$' "$TMP/literal.env" && echo 1 || echo 0)"

# Keys that are not valid environment names are rejected.
if sh "$HELPER" "$TMP/bad.env" 'BAD-KEY' v >/dev/null 2>&1; then
  check "invalid key rejected" 0 "expected a non-zero exit"
else
  check "invalid key rejected" 1
fi

if [ "$FAIL" -ne 0 ]; then
  echo "[env-set-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[env-set-test] all assertions passed"
