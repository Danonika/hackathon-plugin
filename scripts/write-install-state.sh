#!/usr/bin/env sh
# Write configs/install-state.json. Usage: write-install-state.sh <root> <profile> <runtime>
set -e
ROOT=$(CDPATH= cd -- "${1:?missing repository root}" && pwd -P) || exit 1
PROFILE="${2:-full}"
RUNTIME="${3:-unknown}"
if [ -z "$ROOT" ] || [ "$ROOT" = "/" ] || [ ! -f "$ROOT/docker-compose.yml" ] || [ ! -f "$ROOT/go.mod" ]; then
  echo "[install-state] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi
case "$PROFILE" in
  core|full) ;;
  *) echo "[install-state] invalid profile: $PROFILE (expected core or full)" >&2; exit 1 ;;
esac
case "$RUNTIME" in
  binary|docker|unknown) ;;
  *) echo "[install-state] invalid runtime: $RUNTIME (expected binary, docker or unknown)" >&2; exit 1 ;;
esac
mkdir -p "$ROOT/configs"

json_escape() {
  case "$1" in *[[:cntrl:]]*) echo "[install-state] invalid control character" >&2; return 1 ;; esac
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

PLUGIN_JSON="$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"
VERSION=""
if [ -f "$PLUGIN_JSON" ] && command -v python3 >/dev/null 2>&1; then
  VERSION=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' "$PLUGIN_JSON" 2>/dev/null || true)
fi
if [ -z "$VERSION" ] && [ -f "$PLUGIN_JSON" ] && command -v node >/dev/null 2>&1; then
  VERSION=$(node -e 'const v=require(process.argv[1]).version; if(typeof v==="string")console.log(v)' "$PLUGIN_JSON" 2>/dev/null || true)
fi
if [ -z "$VERSION" ]; then
  echo "[install-state] warning: cannot read a version from $PLUGIN_JSON" >&2
  VERSION="unknown"
fi

SKILLS=0
if [ -d "$ROOT/.agents/skills" ]; then
  if LIST=$(find "$ROOT/.agents/skills" -mindepth 1 -maxdepth 1 -type d 2>/dev/null); then
    SKILLS=$(printf '%s\n' "$LIST" | sed '/^$/d' | wc -l | tr -d ' ')
  else
    echo "[install-state] warning: cannot enumerate .agents/skills" >&2
  fi
else
  echo "[install-state] warning: .agents/skills is missing; skills count is 0" >&2
fi

# Component flags are derived from real filesystem evidence (matching the
# PowerShell sibling) rather than hard-coded, so a core/full or partial install
# is described accurately.
HOOKS=false
if [ -f "$ROOT/plugins/hackathonkit/hooks/codex-hooks.json" ]; then HOOKS=true; fi
AGENTS=false
if [ -d "$ROOT/.codex/agents" ] && [ -n "$(find "$ROOT/.codex/agents" -name '*.toml' 2>/dev/null | head -n 1)" ]; then
  AGENTS=true
fi
LEARNING=false
if [ -d "$ROOT/.agents/skills/learning-loop" ]; then LEARNING=true; fi
ADAPTERS_JSON=""
for pair in "codex:AGENTS.md" "opencode:.opencode/opencode.json" "claude:.claude-plugin/plugin.json" "cursor:.cursor/rules/hackathonkit.mdc"; do
  name=${pair%%:*}
  path=${pair#*:}
  if [ -e "$ROOT/$path" ]; then
    if [ -n "$ADAPTERS_JSON" ]; then
      ADAPTERS_JSON="$ADAPTERS_JSON, \"$name\""
    else
      ADAPTERS_JSON="\"$name\""
    fi
  fi
done

STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_JSON=$(json_escape "$VERSION")
PROFILE_JSON=$(json_escape "$PROFILE")
RUNTIME_JSON=$(json_escape "$RUNTIME")
TMP_STATE=$(mktemp "$ROOT/configs/install-state.json.tmp.XXXXXX") || { echo "[install-state] cannot stage install-state.json" >&2; exit 1; }
cleanup_state() { rm -f "$TMP_STATE"; }
trap cleanup_state EXIT
trap 'cleanup_state; exit 130' INT
trap 'cleanup_state; exit 143' TERM
cat > "$TMP_STATE" <<EOF
{
  "version": "$VERSION_JSON",
  "profile": "$PROFILE_JSON",
  "runtime": "$RUNTIME_JSON",
  "skills": $SKILLS,
  "components": {
    "hooks": $HOOKS,
    "agents": $AGENTS,
    "learning": $LEARNING,
    "adapters": [$ADAPTERS_JSON]
  },
  "generated_at": "$STAMP"
}
EOF
chmod 0644 "$TMP_STATE"
mv "$TMP_STATE" "$ROOT/configs/install-state.json"
echo "[install-state] wrote configs/install-state.json (version $VERSION, profile $PROFILE, runtime $RUNTIME)"
