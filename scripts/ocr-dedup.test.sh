#!/usr/bin/env sh
# Regression tests for scripts/ocr-dedup.py (canonical backlog merge).
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
DEDUP="$ROOT/scripts/ocr-dedup.py"
FAIL=0
check() {
  name="$1"; ok="$2"; detail="$3"
  if [ "$ok" = "1" ]; then echo "[ok]   $name"
  else echo "[fail] $name${detail:+: $detail}"; FAIL=$((FAIL + 1)); fi
}

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT INT TERM

# Two scans of the same code: one finding is stable, one is new, and one has the
# same code but a shifted line (must dedup to the same signature).
cat > "$WORK/a.json" <<'JSON'
{"comments":[
 {"path":"x.go","start_line":10,"severity":"high","category":"bug","content":"bug one","existing_code":"a := 1"},
 {"path":"x.go","start_line":20,"severity":"low","category":"style","content":"nit","existing_code":"b := 2"}
]}
JSON
cat > "$WORK/b.json" <<'JSON'
{"comments":[
 {"path":"x.go","start_line":14,"severity":"high","category":"bug","content":"bug one reworded","existing_code":"a := 1"},
 {"path":"y.go","start_line":5,"severity":"medium","category":"bug","content":"bug three","existing_code":"c := 3"}
]}
JSON

python3 "$DEDUP" --baseline "$WORK/base.json" --out-json "$WORK/base.json" "$WORK/a.json" >/dev/null 2>&1
check "first scan creates entries" "$([ "$(python3 -c "import json;print(len(json.load(open('$WORK/base.json'))['findings']))")" = "2" ] && echo 1)"

python3 "$DEDUP" --baseline "$WORK/base.json" --out-json "$WORK/base.json" "$WORK/b.json" >/dev/null 2>&1
TOTAL=$(python3 -c "import json;print(len(json.load(open('$WORK/base.json'))['findings']))")
check "shifted-line finding dedups (same code)" "$([ "$TOTAL" = "3" ] && echo 1)" "got $TOTAL, want 3"

OPEN=$(python3 -c "import json;print(sum(1 for e in json.load(open('$WORK/base.json'))['findings'].values() if e['status']=='open'))")
check "three unique open findings" "$([ "$OPEN" = "3" ] && echo 1)" "got $OPEN"

# Suppress one; a later re-scan must keep it suppressed.
SIG=$(python3 -c "
import json
for s,e in json.load(open('$WORK/base.json'))['findings'].items():
    if e['path']=='y.go': print(s)
")
python3 "$DEDUP" --baseline "$WORK/base.json" --out-json "$WORK/base.json" --set false-positive --match "$SIG" >/dev/null 2>&1
python3 "$DEDUP" --baseline "$WORK/base.json" --out-json "$WORK/base.json" "$WORK/b.json" >/dev/null 2>&1
SUP=$(python3 -c "
import json
e=json.load(open('$WORK/base.json'))['findings']['$SIG']
print(e['status'])
")
check "suppressed status survives a re-scan" "$([ "$SUP" = "false-positive" ] && echo 1)" "got $SUP"
OPEN2=$(python3 -c "import json;print(sum(1 for e in json.load(open('$WORK/base.json'))['findings'].values() if e['status']=='open'))")
check "suppressed finding leaves the open count" "$([ "$OPEN2" = "2" ] && echo 1)" "got $OPEN2"

# An untargeted triage must never suppress the entire baseline.
if python3 "$DEDUP" --baseline "$WORK/base.json" --set fixed >/dev/null 2>&1; then
  check "triage requires a target" 0
fi
if python3 "$DEDUP" --baseline "$WORK/base.json" --set fixed --match no-such-finding >/dev/null 2>&1; then
  check "unknown signature rejected" 0
fi
python3 "$DEDUP" --baseline "$WORK/base.json" --set fixed "$WORK/a.json" >/dev/null 2>&1
python3 - "$WORK/base.json" <<'PYTEST'
import json, sys
entries = json.load(open(sys.argv[1]))['findings'].values()
assert all(e['status'] == ('fixed' if e['path'] == 'x.go' else 'false-positive') for e in entries)
PYTEST

# The replaced frontend is never actionable.
cat > "$WORK/f.json" <<'JSON'
{"comments":[{"path":"cmd/demo-api/static/x.js","start_line":1,"severity":"critical","category":"bug","content":"x","existing_code":"y"}]}
JSON
python3 "$DEDUP" --baseline "$WORK/fbase.json" --out-json "$WORK/fbase.json" "$WORK/f.json" >/dev/null 2>&1
check "frontend findings excluded" "$([ "$(python3 -c "import json;print(len(json.load(open('$WORK/fbase.json'))['findings']))")" = "0" ] && echo 1)"

if [ "$FAIL" -gt 0 ]; then
  echo "[ocr-dedup-test] $FAIL assertion(s) failed"
  exit 1
fi
echo "[ocr-dedup-test] all assertions passed"
