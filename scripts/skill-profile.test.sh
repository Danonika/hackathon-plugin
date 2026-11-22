#!/usr/bin/env sh
# Exercise install, regeneration, doctor and repair without touching a user install.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
repo="$work/repo"
mkdir -p "$repo" "$work/tools" "$work/codex"
for path in scripts plugins/hackathonkit/skills/plan plugins/hackathonkit/skills/judge \
    plugins/hackathonkit/commands plugins/hackathonkit/scripts plugins/hackathonkit/hooks \
    plugins/hackathonkit/.mcp.json.template plugins/hackathonkit/.codex-plugin .codex \
    .claude-plugin .opencode/opencode.json .cursor AGENTS.md docker-compose.yml \
    go.mod .env.example configs/routing.yaml; do
  mkdir -p "$repo/$(dirname "$path")"
  cp -R "$root/$path" "$repo/$path"
done
for tool in codex docker node; do
  printf '#!/bin/sh\nexit 0\n' > "$work/tools/$tool"
  chmod +x "$work/tools/$tool"
done
run() {
  PATH="$work/tools:$PATH" CODEX_HOME="$work/codex" "$@" >"$work/log" 2>&1 || {
    cat "$work/log" >&2; return 1;
  }
}
check_mirrors() {
  for mirror in "$repo/.agents/skills" "$repo/.claude/skills"; do
    [ -f "$mirror/plan/SKILL.md" ] || { echo "missing core skill in $mirror" >&2; exit 1; }
    if [ "$1" = core ]; then
      [ ! -e "$mirror/judge" ] || { echo "explicit-only skill unexpectedly mirrored in $mirror" >&2; exit 1; }
    else [ -f "$mirror/judge/SKILL.md" ] || { echo "missing full-profile skill in $mirror" >&2; exit 1; }; fi
  done
  [ -f "$repo/plugins/hackathonkit/skills/judge/SKILL.md" ]
  python3 - "$repo/configs/install-state.json" "$1" <<'PYTHON'
import json, sys
with open(sys.argv[1], encoding="utf-8-sig") as src:
    state = json.load(src)
assert state["profile"] == sys.argv[2]
assert state["skills"] == (1 if sys.argv[2] == "core" else 2)
PYTHON
}
check_doctor() {
  # Other runtime checks are outside this fixture; inspect the mirror diagnostic.
  PATH="$work/tools:$PATH" CODEX_HOME="$work/codex" sh "$repo/scripts/doctor.sh" >"$work/doctor" 2>&1 || :
  grep -E '^\[ok\][[:space:]]+skills mirror' "$work/doctor" || {
    grep 'skills mirror' "$work/doctor" >&2
    exit 1
  }
}
run sh "$repo/scripts/setup.sh" --profile core
check_mirrors core
printf '{"runtime":"docker","profile":"core","version":"test"}\n' > "$repo/configs/install-state.json"
run sh "$repo/scripts/repair.sh"
check_mirrors core
rm -rf "$repo/.claude/skills/plan"
run sh "$repo/scripts/sync-skills.sh"
run sh "$repo/scripts/sync-harness.sh"
check_mirrors core
check_doctor
run sh "$repo/scripts/setup.sh" --profile full
check_mirrors full
run sh "$repo/scripts/repair.sh"
check_mirrors full
check_doctor
if command -v pwsh >/dev/null 2>&1; then
  run pwsh -NoProfile -File "$repo/scripts/setup.ps1" -Profile Core
  check_mirrors core
  printf '{"runtime":"docker","profile":"core","version":"test"}\n' > "$repo/configs/install-state.json"
  run pwsh -NoProfile -File "$repo/scripts/repair.ps1"
  check_mirrors core
  run pwsh -NoProfile -File "$repo/scripts/sync-skills.ps1"
  rm -rf "$repo/.claude/skills/plan"
  run pwsh -NoProfile -File "$repo/scripts/sync-harness.ps1"
  check_mirrors core
  PATH="$work/tools:$PATH" CODEX_HOME="$work/codex" pwsh -NoProfile -File "$repo/scripts/doctor.ps1" >"$work/doctor" 2>&1 || :
  grep -E '^\[ok\][[:space:]]+skills mirror' "$work/doctor" || { cat "$work/doctor" >&2; exit 1; }
  run pwsh -NoProfile -File "$repo/scripts/setup.ps1" -Profile full
  check_mirrors full
  run pwsh -NoProfile -File "$repo/scripts/repair.ps1"
  check_mirrors full
  echo "[skill-profile-test] PowerShell checks passed"
fi
echo "[skill-profile-test] core and full installs, repairs, syncs and diagnostics passed"
