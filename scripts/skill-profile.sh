# The install profile affects generated skill mirrors only. Plugin policy metadata
# is the source of truth for which skills require explicit invocation.
installed_skill_profile() {
  state_file="$1/configs/install-state.json"
  if [ ! -f "$state_file" ]; then printf '%s\n' full; return; fi
  if command -v python3 >/dev/null 2>&1; then
    installed=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1])).get("profile", ""))' "$state_file" 2>/dev/null) || installed=""
  elif command -v node >/dev/null 2>&1; then
    installed=$(node -p 'JSON.parse(require("fs").readFileSync(process.argv[1], "utf8")).profile' "$state_file" 2>/dev/null) || installed=""
  elif command -v jq >/dev/null 2>&1; then
    installed=$(jq -r '.profile' "$state_file" 2>/dev/null) || installed=""
  else
    # The install-state writer on systems without a JSON runtime emits this field on its own line.
    installed=$(sed -n 's/^[[:space:]]*"profile"[[:space:]]*:[[:space:]]*"\([^"]*\)"[[:space:]]*,\{0,1\}[[:space:]]*$/\1/p' "$state_file")
  fi
  case "$installed" in
    [Cc][Oo][Rr][Ee]) printf '%s\n' core ;;
    [Ff][Uu][Ll][Ll]) printf '%s\n' full ;;
    *) echo "[skills] could not read the install profile; using full" >&2; printf '%s\n' full ;;
  esac
}

skill_in_profile() {
  [ "$2" != core ] || ! grep -Eq '^[[:space:]]+allow_implicit_invocation:[[:space:]]*false([[:space:]]*(#.*)?)?$' "$1/agents/openai.yaml" 2>/dev/null
}

filter_skill_stage() {
  case "$2" in full) return 0 ;; core) ;; *) echo "[skills] invalid profile: $2" >&2; return 1 ;; esac
  for staged_skill in "$1"/*; do
    [ -d "$staged_skill" ] || continue
    if ! skill_in_profile "$staged_skill" "$2"; then rm -rf "$staged_skill"; fi
  done
}
