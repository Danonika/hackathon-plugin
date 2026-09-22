#!/usr/bin/env sh
# Write configs/install-state.json. Usage: write-install-state.sh <root> <profile> <runtime>
set -e
ROOT="$1"
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
ROOT="$(CDPATH= cd -- "$ROOT" && pwd)" || { echo "[install-state] cannot resolve $ROOT" >&2; exit 1; }
mkdir -p "$ROOT/configs"

json_escape() {
  printf '%s' "$1" | tr -d '\r\n' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

PLUGIN_JSON="$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"
VERSION=""
if [ -f "$PLUGIN_JSON" ] && command -v python3 >/dev/null 2>&1; then
  VERSION=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("version",""))' "$PLUGIN_JSON" 2>/dev/null || true)
fi
if [ -z "$VERSION" ]; then
  VERSION=$(sed -n 's/^[[:space:]]*"version": *"\([^"]*\)".*/\1/p' "$PLUGIN_JSON" 2>/dev/null | head -n 1)
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

STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_JSON=$(json_escape "$VERSION")
PROFILE_JSON=$(json_escape "$PROFILE")
RUNTIME_JSON=$(json_escape "$RUNTIME")
TMP_STATE=$(mktemp "$ROOT/configs/install-state.json.tmp.XXXXXX")
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
    "hooks": true,
    "agents": true,
    "learning": true,
    "adapters": ["codex", "opencode", "claude", "cursor"]
  },
  "generated_at": "$STAMP"
}
EOF
chmod 0644 "$TMP_STATE"
mv "$TMP_STATE" "$ROOT/configs/install-state.json"
echo "[install-state] wrote configs/install-state.json (version $VERSION, profile $PROFILE, runtime $RUNTIME)"
