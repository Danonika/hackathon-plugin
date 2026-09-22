#!/usr/bin/env sh
set -e
ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
FAIL=0

check() {
  NAME="$1"
  OK="$2"
  DETAIL="$3"
  if [ "$OK" = "1" ]; then
    echo "[ok]   $NAME $DETAIL"
  else
    echo "[fail] $NAME $DETAIL"
    FAIL=$((FAIL + 1))
  fi
}

if [ -f "$ROOT/docker-compose.yml" ]; then check "repository root" 1 "$ROOT"; else check "repository root" 0 "$ROOT"; fi
if [ -f "$ROOT/configs/routing.yaml" ]; then check "routing config" 1 "$ROOT/configs/routing.yaml"; else check "routing config" 0 "missing"; fi

if command -v go >/dev/null 2>&1; then check "go toolchain (dev)" 1 "$(go version)"; else check "go toolchain (dev)" 1 "not found (not required for docker path)"; fi
if command -v codex >/dev/null 2>&1; then
  check "codex CLI" 1 "$(codex --version 2>/dev/null || echo present)"
  PLUGIN_STATE=missing
  PLUGIN_JSON=$(codex plugin list --json 2>/dev/null || true)
  if [ -n "$PLUGIN_JSON" ]; then
    if command -v jq >/dev/null 2>&1; then
      if printf '%s' "$PLUGIN_JSON" | jq -e '.installed[]? | select(.name == "hackathonkit")' >/dev/null 2>&1; then
        PLUGIN_STATE=disabled
        if printf '%s' "$PLUGIN_JSON" | jq -e '.installed[]? | select(.name == "hackathonkit" and (.enabled != false))' >/dev/null 2>&1; then
          PLUGIN_STATE=enabled
        fi
      fi
    elif command -v python3 >/dev/null 2>&1; then
      PLUGIN_STATE=$(printf '%s' "$PLUGIN_JSON" | python3 -c '
import json, sys
raw = sys.stdin.read()
try:
    data = json.loads(raw)
except Exception:
    print("unknown")
    sys.exit(0)
if not isinstance(data, dict) or not isinstance(data.get("installed"), list):
    print("unknown")
    sys.exit(0)
state = "missing"
for entry in data.get("installed", []):
    if isinstance(entry, dict) and entry.get("name") == "hackathonkit":
        state = "disabled" if entry.get("enabled") is False else "enabled"
print(state)
' 2>/dev/null)
      if [ -z "$PLUGIN_STATE" ]; then
        PLUGIN_STATE=unknown
      fi
    elif PLUGIN_STATE=$(printf '%s' "$PLUGIN_JSON" | awk '
      { text = text $0 }
      END {
        n = split(text, parts, "{")
        for (i = 1; i <= n; i++) {
          if (parts[i] ~ /"name"[ \t]*:[ \t]*"hackathonkit"/) {
            if (parts[i] ~ /"enabled"[ \t]*:[ \t]*false/) { print "disabled"; exit }
            print "enabled"; exit
          }
        }
        if (found) print "enabled"
      }'); then
      if [ -z "$PLUGIN_STATE" ]; then
        PLUGIN_STATE=missing
      fi
    fi
  fi
  PLUGIN_OK=0
  case "$PLUGIN_STATE" in
    enabled) check "hackathonkit plugin" 1 "installed and enabled"; PLUGIN_OK=1 ;;
    disabled) check "hackathonkit plugin" 0 "installed but disabled; enable it in Codex" ;;
    unknown) check "hackathonkit plugin" 0 "could not parse codex plugin list output" ;;
    *) check "hackathonkit plugin" 0 "run scripts/setup.sh" ;;
  esac

  if [ "$PLUGIN_OK" = "1" ]; then
    PLUGIN_RUNTIME=0
    PLUGIN_CHECK_OUT=$(sh "$ROOT/scripts/plugin-check.sh" 2>&1) || PLUGIN_RUNTIME=$?
    case "$PLUGIN_RUNTIME" in
      0) check "plugin runtime" 1 "installed plugin starts MCP from a foreign cwd" ;;
      2) echo "[info] plugin runtime check skipped (codex/python3 unavailable)" ;;
      *) check "plugin runtime" 0 "$(printf '%s' "$PLUGIN_CHECK_OUT" | tail -n 1)" ;;
    esac
  else
    echo "[info] plugin runtime check skipped (plugin not installed or disabled)"
  fi
else
  check "codex CLI" 0 "not found"
fi

DOCKER_OK=0
if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  DOCKER_OK=1
fi
if [ "$DOCKER_OK" = "1" ]; then
  check "docker engine" 1 "running"
else
  check "docker engine" 0 "not running"
fi

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  check "mcp runtime" 1 "local binary"
elif [ "$DOCKER_OK" = "1" ] && docker images --format '{{.Repository}}' 2>/dev/null | grep -q -E '(^|/)hackathon-agent-kit-hackathonkit-mcp$'; then
  check "mcp runtime" 1 "docker image"
else
  check "mcp runtime" 0 "run scripts/setup.sh"
fi

SRC_COUNT=$(find "$ROOT/plugins/hackathonkit/skills" -mindepth 1 -maxdepth 1 -type d 2>/dev/null | wc -l | tr -d ' ')
DST_COUNT=0
SRC_SKILLS=$(find "$ROOT/plugins/hackathonkit/skills" -name SKILL.md 2>/dev/null | wc -l | tr -d ' ')
DST_SKILLS=0
if [ -d "$ROOT/.agents/skills" ]; then
  DST_COUNT=$(find "$ROOT/.agents/skills" -mindepth 1 -maxdepth 1 -type d | wc -l | tr -d ' ')
  DST_SKILLS=$(find "$ROOT/.agents/skills" -name SKILL.md | wc -l | tr -d ' ')
fi
SRC_NAMES=$(cd "$ROOT/plugins/hackathonkit/skills" 2>/dev/null && ls -1d */ 2>/dev/null | sed 's#/##' | sort) || true
DST_NAMES=$(cd "$ROOT/.agents/skills" 2>/dev/null && ls -1d */ 2>/dev/null | sed 's#/##' | sort) || true
if [ "$SRC_COUNT" -gt 0 ] && [ "$SRC_NAMES" = "$DST_NAMES" ] && [ "$SRC_SKILLS" = "$DST_SKILLS" ]; then
  check "skills mirror" 1 "plugin=$SRC_COUNT/$SRC_SKILLS fallback=$DST_COUNT/$DST_SKILLS"
else
  check "skills mirror" 0 "plugin=$SRC_COUNT/$SRC_SKILLS fallback=$DST_COUNT/$DST_SKILLS; run scripts/sync-harness.sh"
fi

if [ -f "$ROOT/plugins/hackathonkit/hooks/codex-hooks.json" ]; then
  check "session hooks" 1 "codex-hooks.json"
else
  check "session hooks" 0 "missing plugins/hackathonkit/hooks/codex-hooks.json"
fi

if [ -f "$ROOT/plugins/hackathonkit/commands/verify.md" ]; then
  check "verify command" 1 "/verify runs 7 checks incl. coverage"
else
  check "verify command" 0 "missing plugins/hackathonkit/commands/verify.md; run scripts/repair.sh"
fi

AGENT_COUNT=0
if [ -d "$ROOT/.codex/agents" ]; then
  AGENT_COUNT=$(find "$ROOT/.codex/agents" -name '*.toml' | wc -l | tr -d ' ')
fi
if [ "$AGENT_COUNT" -ge 3 ]; then
  check "codex subagents" 1 "$AGENT_COUNT roles"
else
  check "codex subagents" 0 "expected 3 roles in .codex/agents"
fi

ADAPTERS_MISSING=0
for f in AGENTS.md .claude-plugin/plugin.json .claude-plugin/marketplace.json .opencode/opencode.json .cursor/rules/hackathonkit.mdc; do
  [ -f "$ROOT/$f" ] || ADAPTERS_MISSING=$((ADAPTERS_MISSING + 1))
done
if [ "$ADAPTERS_MISSING" = "0" ]; then
  check "harness adapters" 1 "codex, opencode, claude, cursor"
else
  check "harness adapters" 0 "$ADAPTERS_MISSING missing; run scripts/sync-harness.sh"
fi

if [ -f "$ROOT/configs/install-state.json" ]; then
  check "install state" 1 "version $(sed -n 's/^[[:space:]]*"version": *"\([^"]*\)".*/\1/p' "$ROOT/configs/install-state.json" | head -n 1)"
else
  check "install state" 0 "missing configs/install-state.json; run scripts/setup.sh"
fi

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  if "$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --validate-skills >/dev/null 2>&1; then
    check "skill standard" 1 "catalog passes docs/SKILL-STANDARD.md"
  else
    check "skill standard" 0 "run scripts/validate-skills.sh for details"
  fi
else
  echo "[info] skill standard check skipped (no local binary; run scripts/setup.sh)"
fi

if [ -n "$TELEGRAM_BOT_TOKEN" ] || grep -q '^TELEGRAM_BOT_TOKEN=.' "$ROOT/.env" 2>/dev/null; then
  echo "[info] telegram             configured (telegram.* tools available)"
else
  echo "[info] telegram             not configured (set TELEGRAM_BOT_TOKEN in .env)"
fi

if [ -x "$ROOT/bin/hackathonkit-mcp" ]; then
  RESOURCES_JSON=$("$ROOT/bin/hackathonkit-mcp" --repo-root "$ROOT" --resources 2>/dev/null || true)
  RESOURCES_PRESENT=false
  RESOURCES_SUMMARY=""
  if command -v jq >/dev/null 2>&1; then
    RESOURCES_PRESENT=$(printf '%s' "$RESOURCES_JSON" | jq -r '.present // false' 2>/dev/null || echo false)
    RESOURCES_SUMMARY=$(printf '%s' "$RESOURCES_JSON" | jq -r '.summary // ""' 2>/dev/null || echo "")
  elif command -v python3 >/dev/null 2>&1; then
    RESOURCES_LINE=$(printf '%s' "$RESOURCES_JSON" | python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    print("false|")
    sys.exit(0)
if not isinstance(d, dict):
    print("false|")
    sys.exit(0)
print(("true" if d.get("present") else "false") + "|" + str(d.get("summary", "")))
' 2>/dev/null || echo "false|")
    RESOURCES_PRESENT=${RESOURCES_LINE%%|*}
    RESOURCES_SUMMARY=${RESOURCES_LINE#*|}
  fi
  if [ "$RESOURCES_PRESENT" = "true" ]; then
    echo "[info] resources            $RESOURCES_SUMMARY"
  else
    echo "[info] resources            not configured (run scripts/setup-resources.sh)"
  fi
elif [ -f "$ROOT/configs/resources.json" ]; then
  echo "[info] resources            profile present (configs/resources.json)"
else
  echo "[info] resources            not configured (run scripts/setup-resources.sh)"
fi

if command -v node >/dev/null 2>&1; then
  check "node (hooks)" 1 "$(node --version 2>/dev/null)"
  if node "$ROOT/plugins/hackathonkit/scripts/hooks.test.js" >/dev/null 2>&1; then
    check "hook behavior" 1 "session ownership test"
  else
    check "hook behavior" 0 "run: node plugins/hackathonkit/scripts/hooks.test.js"
  fi
else
  check "node (hooks)" 0 "not found; session hooks require Node 18+"
fi

SMOKE_OUT=$(sh "$ROOT/scripts/smoke.sh" 2>&1) && SMOKE_STATUS=0 || SMOKE_STATUS=$?
if [ "$SMOKE_STATUS" = "0" ]; then
  check "mcp handshake" 1 "initialize / tools/list / system.context"
else
  check "mcp handshake" 0 "$(printf '%s' "$SMOKE_OUT" | tail -n 1)"
fi

if [ "$FAIL" = "0" ]; then
  echo "[doctor] all checks passed"
  exit 0
fi
echo "[doctor] $FAIL check(s) failed"
exit 1
