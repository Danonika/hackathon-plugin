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
CODEX_DIR="${CODEX_HOME:-$HOME/.codex}"
DEST="$CODEX_DIR/config.toml"
BEGIN="# >>> hackathonkit-mcp (managed by scripts/gen-codex-config.sh) <<<"
END="# <<< hackathonkit-mcp (managed by scripts/gen-codex-config.sh) >>>"
if [ "${1:-}" = "--remove" ] && [ ! -f "$DEST" ]; then
  echo "[codex-config] no user config to clean at $DEST"
  exit 0
fi
mkdir -p "$CODEX_DIR"
. "$(dirname -- "$0")/file-lock.sh"
acquire_file_lock "$CODEX_DIR/.hackathonkit-config.lock"
strip_block() {
  awk -v begin="$BEGIN" -v end="$END" '
    $0 == begin { if (inside) exit 1; inside=1; next }
    $0 == end { if (!inside) exit 1; inside=0; next }
    !inside { print }
    END { if (inside) exit 1 }
  ' "$DEST" || { echo "[codex-config] incomplete block markers in $DEST; keeping the original" >&2; return 1; }
}
if [ "${1:-}" = "--remove" ]; then
  TMP=$(mktemp "$CODEX_DIR/config.toml.tmp.XXXXXX")
  trap 'rm -f "$TMP"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM
  strip_block > "$TMP"
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

case "$LAUNCHER" in
  *[[:cntrl:]]*) echo "[codex-config] launcher path contains a control character" >&2; exit 1 ;;
esac
toml_escape() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g'
}

# Check the unmanaged content again after stripping the generated block below.
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
# Prefer exact TOML values when available; the limited extractor also supports
# older systems and supplies the documented defaults for malformed overrides.
if [ -f "$PROJECT_CONFIG" ] && parsed=$(python3 - "$PROJECT_CONFIG" 2>/dev/null <<'PYTHON'
import sys, tomllib
with open(sys.argv[1], "rb") as stream:
    server = tomllib.load(stream).get("mcp_servers", {}).get("hackathonkit", {})
for key in ("startup_timeout_sec", "tool_timeout_sec"):
    value = server.get(key)
    print(value if type(value) is int and value >= 0 else 180)
PYTHON
); then
  STARTUP_TIMEOUT=$(printf '%s\n' "$parsed" | sed -n '1p')
  TOOL_TIMEOUT=$(printf '%s\n' "$parsed" | sed -n '2p')
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
TMP_TRIM=""
cleanup() {
  rm -f "$TMP"
  if [ -n "$TMP_TRIM" ]; then rm -f "$TMP_TRIM"; fi
}
trap cleanup EXIT
trap 'cleanup; exit 130' INT
trap 'cleanup; exit 143' TERM

# Refuse a partial marker pair rather than silently removing user configuration.
if [ -f "$DEST" ]; then
  strip_block > "$TMP"
else
  : > "$TMP"
fi

# Only migrate an earlier name if the marked block describes this checkout's
# generated launcher. Another checkout, a hand-written registration, and local
# settings added to a block belong to the user.
TOML_MODULE=""
if command -v python3 >/dev/null 2>&1; then
  if python3 -c 'import tomllib' >/dev/null 2>&1; then
    TOML_MODULE="tomllib"
  elif python3 -c 'import tomli' >/dev/null 2>&1; then
    TOML_MODULE="tomli"
  fi
fi
if [ -n "$TOML_MODULE" ]; then
  python3 - "$TOML_MODULE" "$TMP" "$ROOT" <<'PYTHON' || exit 1
import importlib, re, sys
from pathlib import Path

toml = importlib.import_module(sys.argv[1])
config_path, checkout = Path(sys.argv[2]), Path(sys.argv[3]).resolve()
text = config_path.read_text(encoding="utf-8")
config = toml.loads(text)
servers = config.get("mcp_servers", {})
if "hackathonkit" in servers:
    sys.exit("[codex-config] refusing unmanaged hackathonkit server definition")

marker = r" \(managed by scripts/gen-codex-config\.sh\)"
block = re.compile(r"^# >>> ([A-Za-z0-9_-]+)-mcp" + marker + r" <<<\n"
                   r'\[mcp_servers\.\1\]\ncommand = "sh"\nargs = \[[^\n]*\]\n'
                   r"(?:startup_timeout_sec = [0-9]+\n)?(?:tool_timeout_sec = [0-9]+\n)?"
                   r"# <<< \1-mcp" + marker + r" >>>(?:\n|$)", re.MULTILINE)
for match in reversed(list(block.finditer(text))):
    name = match[1]
    server = servers.get(name)
    if server is None:
        continue
    try:
        generated = toml.loads(match[0])["mcp_servers"][name]
    except toml.TOMLDecodeError:
        continue
    if server != generated:
        continue
    args = server.get("args")
    if not isinstance(args, list) or len(args) != 1 or not isinstance(args[0], str):
        continue
    launcher = Path(args[0])
    suffix = ("plugins", name, "scripts", f"{name}-mcp-launcher.sh")
    if not launcher.is_absolute() or launcher.parts[-4:] != suffix or launcher.parents[3].resolve() != checkout:
        continue
    candidate = text[:match.start()] + text[match.end():]
    # A literal string in the user's config may itself contain an example
    # block. Remove only text whose sole TOML effect is the owned registration.
    remaining = toml.loads(candidate)
    if remaining.pop("mcp_servers", {}) != {k: v for k, v in servers.items() if k != name}:
        continue
    if remaining != {k: v for k, v in config.items() if k != "mcp_servers"}:
        continue
    text = candidate
    del servers[name]
config_path.write_text(text, encoding="utf-8")
PYTHON
else
  # The generator can still install a fresh config without Python. A recorded
  # launcher for this checkout needs a parsed merge to protect other settings.
  if grep -Fq '(managed by scripts/gen-codex-config.sh) <<<' "$TMP" &&
     grep -F "args = [\"$(toml_escape "$ROOT/plugins/")" "$TMP" | grep -Fq -- '-mcp-launcher.sh"]'; then
    echo "[codex-config] install a Python TOML parser or remove this checkout's previous generated server block before upgrading" >&2
    exit 1
  fi
  # Rendering without a TOML parser is only safe for unrelated user keys.
  if grep -Eq '^[[:space:]]*[^#]*(mcp_servers.*hackathonkit|mcp_servers[^[:alnum:]_]*=|hackathonkit[^[:alnum:]_]*=)' "$TMP"; then
    echo "[codex-config] install Python 3.11+ to merge an existing MCP configuration safely" >&2
    exit 1
  fi
fi

# Drop trailing blank lines left by the previous block, then append the block
# with one leading blank line, so repeated runs are byte-stable.
TMP_TRIM=$(mktemp "$CODEX_DIR/config.toml.trim.XXXXXX")
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

# Validate the rendered result with the same TOML parser used for the merge.
if [ -n "$TOML_MODULE" ]; then
  if ! python3 -c "import $TOML_MODULE, sys; $TOML_MODULE.load(open(sys.argv[1], 'rb'))" "$TMP" 2>/dev/null; then
    echo "[codex-config] rendered config is not valid TOML; keeping the previous file" >&2
    exit 1
  fi
fi

chmod "$MODE" "$TMP"
mv "$TMP" "$DEST"
echo "[codex-config] pinned hackathonkit MCP to $LAUNCHER in $DEST"
