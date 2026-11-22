#!/usr/bin/env sh
# Regression tests for scripts/env-set.sh: .env permission preservation.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
HELPER="$ROOT/scripts/env-set.sh"
TMP="$(mktemp -d)"
cleanup_env_set_test() { rm -rf "$TMP"; }
trap cleanup_env_set_test EXIT
trap 'cleanup_env_set_test; exit 130' INT
trap 'cleanup_env_set_test; exit 143' TERM
FAIL=0

check() {
  name="$1"
  ok="$2"
  detail="${3:-}"
  if [ "$ok" = "1" ]; then
    echo "[ok]   $name"
  else
    echo "[fail] $name${detail:+: $detail}"
    FAIL=$((FAIL + 1))
  fi
}

# Guard an expected-success helper invocation so a failure produces a clear
# [fail]-style diagnostic instead of aborting under `set -e` with no context.
require_helper() {
  if ! "$@"; then
    echo "[env-set-test] helper invocation failed: $*" >&2
    exit 1
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
require_helper sh "$HELPER" "$TMP/append.env" NEW value
check "append keeps existing keys" "$(grep -q '^KEEP=1$' "$TMP/append.env" && echo 1 || echo 0)"
check "append adds the new key" "$(grep -q '^NEW=value$' "$TMP/append.env" && echo 1 || echo 0)"

# The value can be fed through stdin so secrets stay out of argv.
(umask 022 && printf 'from-stdin' | sh "$HELPER" "$TMP/stdin.env" S) || { echo "[env-set-test] helper failed for stdin.env" >&2; exit 1; }
check "stdin value is stored" "$(grep -q '^S=from-stdin$' "$TMP/stdin.env" && echo 1 || echo 0)"
check "stdin value file is 0600" "$([ "$(file_mode "$TMP/stdin.env")" = "600" ] && echo 1 || echo 0)" "mode=$(file_mode "$TMP/stdin.env")"

# A single trailing newline from stdin is a terminator, not part of the value.
(umask 022 && printf 'trailing\n' | sh "$HELPER" "$TMP/stdin-nl.env" NL) || { echo "[env-set-test] helper failed for stdin-nl.env" >&2; exit 1; }
check "stdin trailing newline is stripped" "$(grep -q '^NL=trailing$' "$TMP/stdin-nl.env" && echo 1 || echo 0)" "got: $(cat "$TMP/stdin-nl.env")"

# Command substitution strips every trailing newline, so multiple newlines are
# stripped too; pin that behaviour down explicitly.
(umask 022 && printf 'multi\n\n\n' | sh "$HELPER" "$TMP/stdin-multinl.env" MNL) || { echo "[env-set-test] helper failed for stdin-multinl.env" >&2; exit 1; }
check "stdin multiple trailing newlines are stripped" "$(grep -q '^MNL=multi$' "$TMP/stdin-multinl.env" && echo 1 || echo 0)" "got: $(cat "$TMP/stdin-multinl.env")"

# A value containing spaces/quotes is quoted so the .env stays parseable.
require_helper sh "$HELPER" "$TMP/quote.env" SPACED 'two words'
check "spaced value is quoted" "$(grep -q '^SPACED="two words"$' "$TMP/quote.env" && echo 1 || echo 0)" "got: $(cat "$TMP/quote.env")"

# Boundary quoting cases: empty, leading/trailing space, '#', and a single quote.
require_helper sh "$HELPER" "$TMP/empty.env" EMPTY ''
check "empty value stays bare" "$(grep -q '^EMPTY=$' "$TMP/empty.env" && echo 1 || echo 0)" "got: $(cat "$TMP/empty.env")"
require_helper sh "$HELPER" "$TMP/ws.env" WS ' padded '
check "leading/trailing spaces are quoted" "$(grep -q '^WS=" padded "$' "$TMP/ws.env" && echo 1 || echo 0)" "got: $(cat "$TMP/ws.env")"
require_helper sh "$HELPER" "$TMP/hash.env" HASH 'a#b'
check "hash value is quoted" "$(grep -q '^HASH="a#b"$' "$TMP/hash.env" && echo 1 || echo 0)" "got: $(cat "$TMP/hash.env")"
require_helper sh "$HELPER" "$TMP/sq.env" SQ "a'b"
check "single-quote value is quoted" "$(grep -q "^SQ=\"a'b\"$" "$TMP/sq.env" && echo 1 || echo 0)" "got: $(cat "$TMP/sq.env")"

# An embedded newline is preserved as an escaped \n inside a quoted value.
(umask 022 && printf 'line1\nline2\n' | sh "$HELPER" "$TMP/multi.env" MULTI) || { echo "[env-set-test] helper failed for multi.env" >&2; exit 1; }
check "embedded newline is escaped" "$(grep -q '^MULTI="line1\\nline2"$' "$TMP/multi.env" && echo 1 || echo 0)" "got: $(cat "$TMP/multi.env")"

# The dotenv format keeps the interior newline as literal backslash-n on a single line.
if ( . "$TMP/multi.env" && [ "$MULTI" = 'line1\nline2' ] ); then
  check "sourced multiline encoding" 1
else
  check "sourced multiline encoding" 0
fi

# Backslashes trigger quoting and are escaped so a sourced .env yields the
# original value ("p\q\r").
require_helper sh "$HELPER" "$TMP/escape.env" B 'p\q\r'
if ( . "$TMP/escape.env" && [ "$B" = 'p\q\r' ] ); then
  check "backslashes round-trip when sourced" 1
else
  check "backslashes round-trip when sourced" 0
fi

# Backtick characters in ordinary shell text must round-trip literally.
require_helper sh "$HELPER" "$TMP/backtick.env" LABEL 'a`b'
if ( . "$TMP/backtick.env" && [ "$LABEL" = 'a`b' ] ); then
  check "literal backtick round-trips" 1
else
  check "literal backtick round-trips" 0
fi
# Command-substitution text must be emitted inert: sourcing the
# file must not execute them.
INJECT_MARKER="$TMP/inject-marker"
rm -f "$INJECT_MARKER"
require_helper sh "$HELPER" "$TMP/inject.env" EVIL "\$(touch $INJECT_MARKER)"
( set -a; . "$TMP/inject.env"; set +a; printf '%s\n' "$EVIL" > "$TMP/evil-value" ) || check "source literal dollar" 0
check "dollar substitution is not executed" "$([ -f "$INJECT_MARKER" ] && echo 0 || echo 1)" "got: $(cat "$TMP/inject.env")"
check "dollar value round-trips" "$([ "$(cat "$TMP/evil-value")" = "\$(touch $INJECT_MARKER)" ] && echo 1 || echo 0)" "got: $(cat "$TMP/evil-value")"
rm -f "$INJECT_MARKER"

# Default-deny quoting: shell metacharacters that used to be emitted bare must
# now be quoted so sourcing the file cannot execute them.
MARKER="$TMP/cmd-marker"
rm -f "$MARKER"
require_helper sh "$HELPER" "$TMP/meta.env" M1 "semi;touch $MARKER"
require_helper sh "$HELPER" "$TMP/meta.env" M2 'and&&and'
require_helper sh "$HELPER" "$TMP/meta.env" M3 'pipe|sh'
require_helper sh "$HELPER" "$TMP/meta.env" M4 'redir>out'
require_helper sh "$HELPER" "$TMP/meta.env" M5 'glob*?[]'
require_helper sh "$HELPER" "$TMP/meta.env" M6 'subshell(){}'
require_helper sh "$HELPER" "$TMP/meta.env" M7 'tilde~and!bang'
( set -a; . "$TMP/meta.env"; set +a; printf '%s\n' "$M1|$M2|$M3|$M4|$M5|$M6|$M7" > "$TMP/meta-values" ) || check "source quoted values" 0
check "metacharacters are quoted" "$(grep -q '^M3="pipe|sh"$' "$TMP/meta.env" && echo 1 || echo 0)" "got: $(cat "$TMP/meta.env")"
check "semicolon payload not executed" "$([ -f "$MARKER" ] && echo 0 || echo 1)" "got: $(cat "$TMP/meta.env")"
check "metachar values round-trip" "$([ "$(cat "$TMP/meta-values")" = "semi;touch $MARKER|and&&and|pipe|sh|redir>out|glob*?[]|subshell(){}|tilde~and!bang" ] && echo 1 || echo 0)" "got: $(cat "$TMP/meta-values")"
rm -f "$MARKER"

# A provably-safe value stays bare (no needless quoting).
require_helper sh "$HELPER" "$TMP/safe.env" S 'safe-value_1.2:3'
check "safe value stays bare" "$(grep -q '^S=safe-value_1.2:3$' "$TMP/safe.env" && echo 1 || echo 0)" "got: $(cat "$TMP/safe.env")"

# Keys are matched literally, not as regexes.
printf 'A_B=old\nA_XB=keep\n' > "$TMP/literal.env"
require_helper sh "$HELPER" "$TMP/literal.env" A_B new
check "literal key is replaced" "$(grep -q '^A_B=new$' "$TMP/literal.env" && echo 1 || echo 0)"
check "lookalike key is untouched" "$(grep -q '^A_XB=keep$' "$TMP/literal.env" && echo 1 || echo 0)"

# Keys that are not valid environment names are rejected.
if sh "$HELPER" "$TMP/bad.env" 'BAD-KEY' v >/dev/null 2>&1; then
  check "invalid key rejected" 0 "expected a non-zero exit"
else
  check "invalid key rejected" 1
fi

# Writers adding different keys must retain each other's changes, including
# when they race on the first creation of the file.
pids=""
for i in 1 2 3 4 5 6 7 8; do
  sh "$HELPER" "$TMP/concurrent.env" "KEY_$i" "$i" &
  pids="$pids $!"
done
for pid in $pids; do
  wait "$pid" || check "concurrent writer completed" 0
done
for i in 1 2 3 4 5 6 7 8; do
  check "concurrent key $i retained" "$(grep -Fqx "KEY_$i=$i" "$TMP/concurrent.env" && echo 1)"
done

if [ "$FAIL" -ne 0 ]; then
  echo "[env-set-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[env-set-test] all assertions passed"
