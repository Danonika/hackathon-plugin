#!/usr/bin/env sh
# Regression tests for scripts/redact.sh (shared credential redactor).
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
REDACT="$ROOT/scripts/redact.sh"
FAIL=0
check() {
  name="$1"; ok="$2"; detail="$3"
  if [ "$ok" = "1" ]; then echo "[ok]   $name"
  else echo "[fail] $name${detail:+: $detail}"; FAIL=$((FAIL + 1)); fi
}

out=$(printf 'postgres://user:s3cr3t@db:5432/app\n' | sh "$REDACT")
check "url userinfo redacted" "$(printf '%s' "$out" | grep -q '://\*\*\*@db' && ! printf '%s' "$out" | grep -q 's3cr3t' && echo 1)" "got: $out"

out=$(printf 'POSTGRES_PASSWORD="a b"\n' | sh "$REDACT")
check "quoted value redacted whole" "$(printf '%s' "$out" | grep -q 'POSTGRES_PASSWORD=\[redacted\]' && ! printf '%s' "$out" | grep -q 'a b' && echo 1)" "got: $out"

out=$(printf 'API_KEY=abc123\n' | sh "$REDACT")
check "mixed-case key redacted" "$(printf '%s' "$out" | grep -q 'API_KEY=\[redacted\]' && echo 1)" "got: $out"

out=$(printf 'secret: hunter2\ntoken=xyz\n' | sh "$REDACT")
check "colon + underscore keys redacted" "$(printf '%s' "$out" | grep -q 'secret: \[redacted\]' && printf '%s' "$out" | grep -q 'token=\[redacted\]' && echo 1)" "got: $out"

out=$(printf 'plain=value\npath=/x/y\n' | sh "$REDACT")
check "benign content unchanged" "$(printf '%s' "$out" | grep -q 'plain=value' && printf '%s' "$out" | grep -q 'path=/x/y' && echo 1)" "got: $out"

if [ "$FAIL" -gt 0 ]; then
  echo "[redact-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[redact-test] all assertions passed"
