#!/usr/bin/env sh
# Write configs/install-state.json. Usage: write-install-state.sh <root> <profile> <runtime>
set -e
ROOT="$1"
PROFILE="${2:-full}"
RUNTIME="${3:-unknown}"
mkdir -p "$ROOT/configs"

PLUGIN_JSON="$ROOT/plugins/hackathonkit/.codex-plugin/plugin.json"
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$PLUGIN_JSON" 2>/dev/null | head -n 1)
if [ -z "$VERSION" ]; then
  echo "[install-state] warning: cannot read a version from $PLUGIN_JSON" >&2
  VERSION="unknown"
fi

SKILLS=0
if [ -d "$ROOT/.agents/skills" ]; then
  SKILLS=$(find "$ROOT/.agents/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
fi

STAMP=$(date -u +%Y-%m-%dT%H:%M:%SZ)
cat > "$ROOT/configs/install-state.json" <<EOF
{
  "version": "$VERSION",
  "profile": "$PROFILE",
  "runtime": "$RUNTIME",
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
echo "[install-state] wrote configs/install-state.json (version $VERSION, profile $PROFILE, runtime $RUNTIME)"
