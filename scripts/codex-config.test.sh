#!/usr/bin/env sh
# Regression tests for .codex/config.toml placement and the user-config generator.
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FAIL=0
check() {
  name="$1"; ok="$2"; detail="$3"
  if [ "$ok" = "1" ]; then echo "[ok]   $name"
  else echo "[fail] $name${detail:+: $detail}"; FAIL=$((FAIL + 1)); fi
}
need_python() {
  if ! command -v python3 >/dev/null 2>&1; then
    echo "[codex-config-test] python3 required for this suite" >&2
    exit 2
  fi
}
need_python

CONFIG="$ROOT/.codex/config.toml"
# Capture the tracked config BEFORE any generator run so the untouched assertion
# can actually catch a regression.
BEFORE_HASH=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$CONFIG")

# Root-level keys must be parsed as top-level, not swallowed by the server table
# (TOML table scoping: a key after [mcp_servers.hackathonkit] belongs to that table).
TOP=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
server = d.get("mcp_servers", {}).get("hackathonkit", {})
print("ok" if "web_search" in d and "web_search" not in server else "bad")
' "$CONFIG")
check "web_search is a root-level key" "$([ "$TOP" = "ok" ] && echo 1)" "got: $TOP"

# The committed config must stay machine-neutral (no absolute launcher path).
COMMITTED_ARGS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
print(d["mcp_servers"]["hackathonkit"]["args"][0])
' "$CONFIG")
check "committed config is not machine-specific" "$(printf '%s' "$COMMITTED_ARGS" | grep -q "^/" && echo 0 || echo 1)" "got: $COMMITTED_ARGS"

# The generator writes an absolute launcher path to the USER config only.
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"; exit 130' INT TERM
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/plugins/hackathonkit/scripts" "$WORK/codexhome"
: > "$WORK/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
printf '# user config\napproval_policy = "on-request"\n' > "$WORK/codexhome/config.toml"
CODEX_HOME="$WORK/codexhome" sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1
USER_ARGS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
server = d["mcp_servers"]["hackathonkit"]
print("root|" + server["args"][0])
print("ok" if d.get("approval_policy") == "on-request" else "lost")
' "$WORK/codexhome/config.toml")
check "generator writes an absolute launcher path" "$(printf '%s' "$USER_ARGS" | head -1 | grep -q "^root|/" && echo 1)" "got: $USER_ARGS"
check "generator preserves user keys" "$(printf '%s' "$USER_ARGS" | grep -q "^ok$" && echo 1)" "got: $USER_ARGS"

# Re-running must not duplicate the managed block and must be byte-stable.
HASH_ONE=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/codexhome/config.toml")
CODEX_HOME="$WORK/codexhome" sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1
HASH_TWO=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/codexhome/config.toml")
COUNT=$(grep -c "hackathonkit-mcp (managed by" "$WORK/codexhome/config.toml" || true)
check "generator is idempotent" "$([ "$COUNT" = "2" ] && echo 1)" "marker lines: $COUNT"
check "generator output is byte-stable" "$([ "$HASH_ONE" = "$HASH_TWO" ] && echo 1)" "rerun changed the file"

# A hand-written hackathonkit table without markers must be refused, not overwritten.
printf '[mcp_servers.hackathonkit]\ncommand = "custom"\n' > "$WORK/codexhome/handwritten.toml"
mkdir -p "$WORK/hand"
if CODEX_HOME="$WORK/hand" sh -c 'mkdir -p "$CODEX_HOME"; cp "$1" "$CODEX_HOME/config.toml"; sh "$2" "$3"' sh "$WORK/codexhome/handwritten.toml" "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1; then
  check "hand-written table refused" 0 "generator overwrote a hand-written table"
else
  check "hand-written table refused" 1
fi

# --remove strips the managed block and keeps user content.
CODEX_HOME="$WORK/codexhome" sh "$ROOT/scripts/gen-codex-config.sh" --remove >/dev/null 2>&1
check "remove drops the managed block" "$(grep -q "hackathonkit-mcp (managed by" "$WORK/codexhome/config.toml" && echo 0 || echo 1)" "block still present"
check "remove keeps user keys" "$(grep -q 'approval_policy = "on-request"' "$WORK/codexhome/config.toml" && echo 1)" "user key lost"

# The tracked project config must be unchanged by every generator run above.
AFTER_HASH=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$CONFIG")
check "tracked config untouched" "$([ "$BEFORE_HASH" = "$AFTER_HASH" ] && echo 1)" "generator modified a tracked file"

if [ "$FAIL" -gt 0 ]; then
  echo "[codex-config-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[codex-config-test] all assertions passed"
