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
  if ! python3 -c "import tomllib" >/dev/null 2>&1; then
    echo "[codex-config-test] Python 3.11+ with tomllib required for this suite" >&2
    exit 2
  fi
}
need_python

# Invoke the generator as an assertion rather than a bare command: under `set -e`
# a non-zero exit would abort the suite and skip every later check and the
# failure summary.
run_generator() {
  name="$1"; home="$2"; root="$3"
  if CODEX_HOME="$home" sh "$ROOT/scripts/gen-codex-config.sh" "$root" >/dev/null 2>&1; then
    check "$name" 1
  else
    check "$name" 0 "generator exited non-zero"
  fi
}

CONFIG="$ROOT/.codex/config.toml"
# Capture the tracked config BEFORE any generator run so the untouched assertion
# can actually catch a regression.
BEFORE_HASH=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$CONFIG") || { check "read BEFORE_HASH" 0 "could not read config"; BEFORE_HASH=""; }

# Root-level keys must be parsed as top-level, not swallowed by the server table
# (TOML table scoping: a key after [mcp_servers.hackathonkit] belongs to that table).
TOP=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
server = d.get("mcp_servers", {}).get("hackathonkit", {})
print("ok" if "web_search" in d and "web_search" not in server else "bad")
' "$CONFIG") || { check "read TOP" 0 "could not read config"; TOP=""; }
check "web_search is a root-level key" "$([ "$TOP" = "ok" ] && echo 1)" "got: $TOP"

# The committed config must stay machine-neutral (no absolute launcher path).
COMMITTED_ARGS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
print(d["mcp_servers"]["hackathonkit"]["args"][0])
' "$CONFIG") || { check "read COMMITTED_ARGS" 0 "could not read config"; COMMITTED_ARGS=""; }
check "committed config is not machine-specific" "$(printf '%s' "$COMMITTED_ARGS" | grep -q "^/" && echo 0 || echo 1)" "got: $COMMITTED_ARGS"

# The generator writes an absolute launcher path to the USER config only.
WORK=$(mktemp -d)
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/plugins/hackathonkit/scripts" "$WORK/codexhome" "$WORK/.codex"
: > "$WORK/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
# A scratch project config with non-default budgets plus a decoy table (other
# budgets must be ignored) and a multi-line array bracket (must not reset the
# table scope).
cat > "$WORK/.codex/config.toml" <<'CFG'
#:schema https://developers.openai.com/codex/config-schema.json
web_search = "disabled"
[mcp_servers.other]
startup_timeout_sec = 999
tool_timeout_sec = 999
[mcp_servers.hackathonkit]
command = "sh"
args = [
  "plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh",
]
startup_timeout_sec = 245
tool_timeout_sec = 310
CFG
printf '# user config\napproval_policy = "on-request"\n' > "$WORK/codexhome/config.toml"
run_generator "generator succeeds on the scratch project config" "$WORK/codexhome" "$WORK"
USER_ARGS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
server = d["mcp_servers"]["hackathonkit"]
print("root|" + server["args"][0])
print("ok" if d.get("approval_policy") == "on-request" else "lost")
' "$WORK/codexhome/config.toml") || { check "read USER_ARGS" 0 "could not read config"; USER_ARGS=""; }
check "generator writes an absolute launcher path" "$(printf '%s' "$USER_ARGS" | head -1 | grep -q "^root|/" && echo 1)" "got: $USER_ARGS"
check "generator preserves user keys" "$(printf '%s' "$USER_ARGS" | grep -q "^ok$" && echo 1)" "got: $USER_ARGS"

# The managed block must carry the budgets derived from the project config (not
# hardcoded defaults), so the override cannot silently shorten them.
BUDGETS=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
s = d["mcp_servers"]["hackathonkit"]
print("ok" if s.get("startup_timeout_sec") == 245 and s.get("tool_timeout_sec") == 310 else "bad:%r/%r" % (s.get("startup_timeout_sec"), s.get("tool_timeout_sec")))
' "$WORK/codexhome/config.toml") || { check "read BUDGETS" 0 "could not read config"; BUDGETS=""; }
check "managed block derives project budgets" "$([ "$BUDGETS" = "ok" ] && echo 1)" "got: $BUDGETS"

# A brand-new user config (no pre-existing file) must be created private (0600);
# it may hold credentials. A pre-existing file keeps its own mode.
mkdir -p "$WORK/fresh"
run_generator "generator succeeds for a brand-new user config" "$WORK/fresh" "$WORK"
NEWMODE=$(stat -c '%a' "$WORK/fresh/config.toml" 2>/dev/null || stat -f '%Lp' "$WORK/fresh/config.toml" 2>/dev/null || echo "")
check "new user config is private" "$([ "$NEWMODE" = "600" ] && echo 1)" "mode: $NEWMODE"

# A pre-existing mode is preserved (e.g. a user file is already 0600 or 0640).
mkdir -p "$WORK/existing"
printf '# user\n' > "$WORK/existing/config.toml"
chmod 640 "$WORK/existing/config.toml"
run_generator "generator succeeds for an existing user config" "$WORK/existing" "$WORK"
KEPTMODE=$(stat -c '%a' "$WORK/existing/config.toml" 2>/dev/null || stat -f '%Lp' "$WORK/existing/config.toml" 2>/dev/null || echo "")
check "existing config mode preserved" "$([ "$KEPTMODE" = "640" ] && echo 1)" "mode: $KEPTMODE"

# Inspect the trim scratch file at the instant it becomes the next staging
# file. A relaxed caller umask must not expose the original config's contents.
mkdir -p "$WORK/mode-probe/bin" "$WORK/mode-probe/config"
printf 'user_setting = true\n\n' > "$WORK/mode-probe/config/config.toml"
chmod 600 "$WORK/mode-probe/config/config.toml"
cat > "$WORK/mode-probe/bin/mv" <<'SH'
#!/bin/sh
case "$2" in
  "$CODEX_HOME"/config.toml.tmp.*)
    (stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1") > "$MODE_RECORD"
    ;;
esac
exec "$ORIGINAL_MV" "$@"
SH
chmod 700 "$WORK/mode-probe/bin/mv"
if (umask 000; ORIGINAL_MV="$(command -v mv)" MODE_RECORD="$WORK/mode-probe/mode" \
    PATH="$WORK/mode-probe/bin:$PATH" CODEX_HOME="$WORK/mode-probe/config" \
    sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1) &&
   [ "$(cat "$WORK/mode-probe/mode" 2>/dev/null)" = 600 ] &&
   grep -qx 'user_setting = true' "$WORK/mode-probe/config/config.toml"; then
  check "trim scratch stays private with a permissive umask" 1
else
  check "trim scratch stays private with a permissive umask" 0 "scratch permissions or preserved setting differed"
fi

# Re-running must not duplicate the managed block and must be byte-stable.
HASH_ONE=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/codexhome/config.toml") || { check "read HASH_ONE" 0 "could not read config"; HASH_ONE=""; }
run_generator "idempotency re-run succeeds" "$WORK/codexhome" "$WORK"
HASH_TWO=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$WORK/codexhome/config.toml") || { check "read HASH_TWO" 0 "could not read config"; HASH_TWO=""; }
COUNT=$(grep -c "hackathonkit-mcp (managed by" "$WORK/codexhome/config.toml" || true)
check "generator is idempotent" "$([ "$COUNT" = "2" ] && echo 1)" "marker lines: $COUNT"
check "generator output is byte-stable" "$([ "$HASH_ONE" = "$HASH_TWO" ] && echo 1)" "rerun changed the file"

# Earlier versions generated exactly the same block with their own server name.
# Only the block whose launcher belongs to this checkout should migrate.
mkdir -p "$WORK/upgrade" "$WORK/another checkout"
python3 - "$WORK" <<'PYTHON'
from pathlib import Path
import json, sys
root = Path(sys.argv[1])
def block(name, checkout, custom=""):
    marker = f"{name}-mcp (managed by scripts/gen-codex-config.sh)"
    launcher = checkout / "plugins" / name / "scripts" / f"{name}-mcp-launcher.sh"
    return (f"# >>> {marker} <<<\n[mcp_servers.{name}]\ncommand = \"sh\"\n"
            f"args = [{json.dumps(str(launcher))}]\nstartup_timeout_sec = 180\n"
            f"tool_timeout_sec = 180\n{custom}# <<< {marker} >>>\n")
other = block("another-kit", root / "another checkout")
custom = block("custom-kit", root, 'description = "custom setting"\n')
literal = block("quoted-kit", root)
config = ("notes = '''\n" + literal + "'''\n" + other + custom +
          '[mcp_servers.manual]\ncommand = "custom"\n' + block("former-kit", root))
(root / "upgrade/config.toml").write_text(config)
PYTHON
run_generator "generator migrates a previous name from this checkout" "$WORK/upgrade" "$WORK"
if python3 - "$WORK/upgrade/config.toml" <<'PYTHON'
import sys, tomllib
with open(sys.argv[1], "rb") as stream:
    doc = tomllib.load(stream)
assert set(doc["mcp_servers"]) == {"hackathonkit", "another-kit", "manual", "custom-kit"}
assert '# >>> quoted-kit-mcp' in doc["notes"]
assert doc["mcp_servers"]["custom-kit"]["description"] == "custom setting"
PYTHON
then check "only the previous managed registration was replaced" 1
else check "only the previous managed registration was replaced" 0 "duplicate or unrelated config changed"
fi

# A hand-written hackathonkit table without markers must be refused and left intact.
mkdir -p "$WORK/hand"
printf '[mcp_servers.hackathonkit]\ncommand = "custom"\n' > "$WORK/hand/config.toml"
if CODEX_HOME="$WORK/hand" sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1; then
  check "hand-written table refused" 0 "generator exited 0 on a hand-written table"
else
  check "hand-written table refused" 1
fi
check "hand-written table left intact" "$(grep -q 'command = "custom"' "$WORK/hand/config.toml" && echo 1)" "the user's table was rewritten"

# A whitespace-variant header must also be refused.
mkdir -p "$WORK/hand2"
printf '[ mcp_servers . hackathonkit ]\ncommand = "custom"\n' > "$WORK/hand2/config.toml"
if CODEX_HOME="$WORK/hand2" sh "$ROOT/scripts/gen-codex-config.sh" "$WORK" >/dev/null 2>&1; then
  check "spaced hand-written table refused" 0 "generator exited 0"
else
  check "spaced hand-written table refused" 1
fi

# --remove strips the managed block and keeps user content. Guard the call so a
# generator regression is reported as a failed assertion, not a set -e abort.
if CODEX_HOME="$WORK/codexhome" sh "$ROOT/scripts/gen-codex-config.sh" --remove >/dev/null 2>&1; then
  check "generator --remove exits 0" 1
else
  check "generator --remove exits 0" 0 "non-zero exit"
fi
check "remove drops the managed block" "$(grep -q "hackathonkit-mcp (managed by" "$WORK/codexhome/config.toml" && echo 0 || echo 1)" "block still present"
check "remove keeps user keys" "$(grep -q 'approval_policy = "on-request"' "$WORK/codexhome/config.toml" && echo 1)" "user key lost"

# The tracked project config must be unchanged by every generator run above.
AFTER_HASH=$(python3 -c 'import hashlib,sys;print(hashlib.sha256(open(sys.argv[1],"rb").read()).hexdigest())' "$CONFIG") || { check "read AFTER_HASH" 0 "could not read config"; AFTER_HASH=""; }
check "tracked config untouched" "$([ "$BEFORE_HASH" = "$AFTER_HASH" ] && echo 1)" "generator modified a tracked file"

# The budget extraction must reject non-numeric values (the old eval was an
# injection vector): a hostile value falls back to the default.
mkdir -p "$WORK/inject/plugins/hackathonkit/scripts" "$WORK/inject/.codex" "$WORK/cxhome"
: > "$WORK/inject/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
MARKER="$WORK/injection-marker"
{
  printf '[mcp_servers.hackathonkit]\n'
  printf 'startup_timeout_sec = 999;touch %s\n' "$MARKER"
  printf 'tool_timeout_sec = invalid\n'
} > "$WORK/inject/.codex/config.toml"
rm -f "$MARKER"
run_generator "generator succeeds on the injection-safety project" "$WORK/cxhome" "$WORK/inject"
INJ=$(python3 -c '
import tomllib, sys
d = tomllib.load(open(sys.argv[1], "rb"))
s = d["mcp_servers"]["hackathonkit"]
print("%s/%s" % (s.get("startup_timeout_sec"), s.get("tool_timeout_sec")))
' "$WORK/cxhome/config.toml") || { check "read INJ" 0 "could not read config"; INJ=""; }
check "hostile budget value falls back to default" "$([ "$INJ" = "180/180" ] && echo 1)" "got: $INJ"
check "budget extraction does not execute" "$([ -f "$MARKER" ] && echo 0 || echo 1)" "injection executed"
rm -f "$MARKER"

if [ "$FAIL" -gt 0 ]; then
  echo "[codex-config-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[codex-config-test] all assertions passed"
