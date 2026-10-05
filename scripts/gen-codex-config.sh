#!/usr/bin/env sh
# Pin the Hackathon Agent Kit MCP launcher to an absolute path in the USER-level Codex
# config ($CODEX_HOME/config.toml, default ~/.codex/config.toml).
#
# The committed project file (.codex/config.toml) stays machine-neutral and
# cwd-relative; this writes the machine-specific override to the user config,
# which Codex applies with higher precedence. Never rewrites a tracked file.
#
# The block is delimited by markers and replaced on every run (idempotent).
# `scripts/uninstall.sh` can remove it again.
#
# Usage: scripts/gen-codex-config.sh [repo-root]
#        scripts/gen-codex-config.sh --remove
set -e
if [ "$1" = "--remove" ]; then
  CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
  DEST="$CODEX_DIR/config.toml"
  BEGIN="# >>> hackathonkit-mcp (managed by scripts/gen-codex-config.sh) <<<"
  END="# <<< hackathonkit-mcp (managed by scripts/gen-codex-config.sh) >>>"
  [ -f "$DEST" ] || { echo "[codex-config] no user config to clean at $DEST"; exit 0; }
  TMP=$(mktemp "$CODEX_DIR/config.toml.tmp.XXXXXX")
  trap 'rm -f "$TMP"' EXIT
  awk -v begin="$BEGIN" -v end="$END" '$0==begin{inside=1;next} inside&&$0==end{inside=0;next} !inside{print}' "$DEST" > "$TMP"
  chmod "$(stat -c '%a' "$DEST" 2>/dev/null || stat -f '%Lp' "$DEST" 2>/dev/null || echo 600)" "$TMP" 2>/dev/null || true
  mv "$TMP" "$DEST"
  echo "[codex-config] removed the managed hackathonkit block from $DEST"
  exit 0
fi
ROOT="${1:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"

if [ -z "$ROOT" ] || [ ! -d "$ROOT" ]; then
  echo "[codex-config] repository root not found: '$ROOT'" >&2
  exit 1
fi
ROOT="$(CDPATH= cd -- "$ROOT" && pwd)"
LAUNCHER="$ROOT/plugins/hackathonkit/scripts/hackathonkit-mcp-launcher.sh"
if [ ! -f "$LAUNCHER" ]; then
  echo "[codex-config] launcher not found: $LAUNCHER" >&2
  exit 1
fi

CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
DEST="$CODEX_DIR/config.toml"
BEGIN="# >>> hackathonkit-mcp (managed by scripts/gen-codex-config.sh) <<<"
END="# <<< hackathonkit-mcp (managed by scripts/gen-codex-config.sh) >>>"

toml_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

mkdir -p "$CODEX_DIR"

# Refuse to touch a hand-written hackathonkit table: we cannot safely merge it.
# Allow leading whitespace before the header (TOML permits it).
if [ -f "$DEST" ] && ! grep -qF "$BEGIN" "$DEST" &&
   grep -qE '^[[:space:]]*\[[[:space:]]*"?mcp_servers"?[[:space:]]*\.[[:space:]]*"?hackathonkit"?[[:space:]]*\]' "$DEST"; then
  echo "[codex-config] $DEST already defines [mcp_servers.hackathonkit] without the managed markers." >&2
  echo "[codex-config] Remove or rename that table, then re-run; refusing to overwrite it." >&2
  exit 1
fi

# Derive the timeout budgets from the committed config so the override cannot
# silently shorten them when the project defaults change. One scan: enter the
# [mcp_servers.hackathonkit] table, capture the two keys, leave on the next table
# header. Output is a bare integer on its own line, read with `read` (never
# `eval`), so a hostile value cannot execute.
PROJECT_CONFIG="$ROOT/.codex/config.toml"
STARTUP_TIMEOUT=""
TOOL_TIMEOUT=""
if [ -f "$PROJECT_CONFIG" ]; then
  BUDGETS=$(awk '
    # A table header: the whole line is `[ key (. key)? ]` with optional
    # spaces, optional double quotes around each key, and an optional trailing
    # comment. A multi-line array element such as `  [3, 4]` has a comma/space
    # and does not match.
    function table_header(line) {
      return line ~ /^[[:space:]]*\[[[:space:]]*"?[A-Za-z_][A-Za-z0-9_-]*"?([[:space:]]*\.[[:space:]]*"?[A-Za-z_][A-Za-z0-9_-]*"?)*[[:space:]]*\][[:space:]]*(#.*)?$/
    }
    # Whole-line comment or blank lines never change table scope.
    /^[[:space:]]*(#|$)/ { next }
    table_header($0) {
      in_target = ($0 ~ /mcp_servers/ && $0 ~ /hackathonkit/) ? 1 : 0
      next
    }
    in_target && /^[[:space:]]*startup_timeout_sec[[:space:]]*=/ { print "S " int_of($0) }
    in_target && /^[[:space:]]*tool_timeout_sec[[:space:]]*=/ { print "T " int_of($0) }
    function int_of(   v) {
      v = $0
      sub(/^[^=]*=[[:space:]]*/, "", v)
      sub(/[[:space:]#].*$/, "", v)
      # Accept only a run of digits; anything else becomes empty.
      if (v ~ /^[0-9]+$/) return v
      return ""
    }
  ' "$PROJECT_CONFIG")
  STARTUP_TIMEOUT=$(printf '%s\n' "$BUDGETS" | awk '$1=="S"{print $2; exit}')
  TOOL_TIMEOUT=$(printf '%s\n' "$BUDGETS" | awk '$1=="T"{print $2; exit}')
fi
case "$STARTUP_TIMEOUT" in ''|*[!0-9]*) STARTUP_TIMEOUT=180 ;; esac
case "$TOOL_TIMEOUT" in ''|*[!0-9]*) TOOL_TIMEOUT=180 ;; esac

# Preserve the destination mode when it exists (it may hold credentials); a new
# file is private.
MODE="600"
if [ -f "$DEST" ]; then
  MODE=$(stat -c '%a' "$DEST" 2>/dev/null || stat -f '%Lp' "$DEST" 2>/dev/null || true)
  [ -n "$MODE" ] || MODE="600"
fi

TMP=$(mktemp "$CODEX_DIR/config.toml.tmp.XXXXXX")
TMP_TRIM="$TMP.trim"
cleanup() { rm -f "$TMP" "$TMP_TRIM"; }
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

# Drop a complete managed block (BEGIN..END). A lone BEGIN with no END means a
# previously interrupted write: drop from BEGIN to EOF so it cannot corrupt the
# file on the next run.
if [ -f "$DEST" ]; then
  awk -v begin="$BEGIN" -v end="$END" '
    $0 == begin { inside = 1; next }
    inside && $0 == end { inside = 0; next }
    !inside { print }
  ' "$DEST" > "$TMP"
else
  : > "$TMP"
fi

# Drop trailing blank lines left by the previous block, then append the block
# with one leading blank line, so repeated runs are byte-stable.
awk 'BEGIN{b=""} /^[[:blank:]]*$/{b=b $0 "\n"; next} {printf "%s%s\n", b, $0; b=""} END{}' "$TMP" > "$TMP_TRIM"
mv "$TMP_TRIM" "$TMP"

{
  printf '\n%s\n' "$BEGIN"
  printf '[mcp_servers.hackathonkit]\n'
  printf 'command = "sh"\n'
  printf 'args = ["%s"]\n' "$(toml_escape "$LAUNCHER")"
  printf 'startup_timeout_sec = %s\n' "$STARTUP_TIMEOUT"
  printf 'tool_timeout_sec = %s\n' "$TOOL_TIMEOUT"
  printf '%s\n' "$END"
} >> "$TMP"

# Validate the rendered TOML when a parser is available. `tomllib` is Python
# 3.11+; older hosts may ship `tomli`. When neither is importable, skip the
# optional check rather than misreporting a missing module as invalid TOML.
TOML_MODULE=""
if command -v python3 >/dev/null 2>&1; then
  if python3 -c 'import tomllib' >/dev/null 2>&1; then
    TOML_MODULE="tomllib"
  elif python3 -c 'import tomli' >/dev/null 2>&1; then
    TOML_MODULE="tomli"
  fi
fi
if [ -n "$TOML_MODULE" ]; then
  if ! python3 -c "import $TOML_MODULE, sys; $TOML_MODULE.load(open(sys.argv[1], 'rb'))" "$TMP" 2>/dev/null; then
    echo "[codex-config] rendered config is not valid TOML; keeping the previous file" >&2
    exit 1
  fi
fi

chmod "$MODE" "$TMP"
mv "$TMP" "$DEST"
echo "[codex-config] pinned hackathonkit MCP to $LAUNCHER in $DEST"
