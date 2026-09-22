#!/usr/bin/env sh
# Write configs/install-state.json. Usage: write-install-state.sh <root> <profile> <runtime>
set -e
ROOT="$1"
PROFILE="${2:-full}"
RUNTIME="${3:-unknown}"
if [ -z "$ROOT" ] || [ "$ROOT" = "/" ] || [ ! -f "$ROOT/docker-compose.yml" ]; then
  echo "[install-state] refusing: '$ROOT' does not look like the hackathonkit repository root" >&2
  exit 1
fi
mkdir -p "$ROOT/configs"

json_escape() {
  printf '%s' "$1" | tr -d '\r\n' | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

PLUGIN_JSON="$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"
VERSION=$(sed -n 's/^[[:space:]]*"version": *"\([^"]*\)".*/\1/p' "$PLUGIN_JSON" 2>/dev/null | head -n 1)
if [ -z "$VERSION" ]; then
  echo "[install-state] warning: cannot read a version from $PLUGIN_JSON" >&2
  VERSION="unknown"
fi

SKILLS=0
if [ -d "$ROOT/.agents/skills" ]; then
  SKILLS=$(find "$ROOT/.agents/skills" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
fi
if ! [ -d "$ROOT/.agents/skills" ]; then
  echo "[install-state] warning: .agents/skills is missing; skills count is 0" >&2
fi

STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
VERSION_JSON=$(json_escape "$VERSION")
PROFILE_JSON=$(json_escape "$PROFILE")
RUNTIME_JSON=$(json_escape "$RUNTIME")
TMP_STATE=$(mktemp "$ROOT/configs/install-state.json.tmp.XXXXXX")
trap 'rm -f "$TMP_STATE"' EXIT INT TERM
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
mv "$TMP_STATE" "$ROOT/configs/install-state.json"
echo "[install-state] wrote configs/install-state.json (version $VERSION, profile $PROFILE, runtime $RUNTIME)"
