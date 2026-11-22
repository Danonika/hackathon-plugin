#!/usr/bin/env sh
# Shared credential redactor. Filters stdin -> stdout, replacing:
#   - URL userinfo (scheme://user:pass@host -> scheme://***@host). The match is
#     bounded by whitespace and takes the last '@' in the token, so a password
#     containing '@', '?', '#' or '/' is fully masked. This can over-redact a
#     benign `user@host` inside a path or query string; over-redaction is
#     preferred to a leak.
#   - <keyword>=<value> / <keyword>: <value> for password/pass/token/secret/
#     dsn/key (case-insensitive), with double-quoted, single-quoted or unquoted
#     values (an unquoted assignment is masked through the end of the line).
#   - JSON form: "keyword": "value"
#
# Fails closed: if `sed -E` is unavailable it emits a notice and no input, so
# callers never print unredacted secrets by accident.
set -e
if ! sed -E 's/a/b/' </dev/null >/dev/null 2>&1; then
  echo "[redact] sed -E unavailable; output withheld to avoid leaking credentials" >&2
  exit 1
fi
KEY='([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss][Ss]|[Tt][Oo][Kk][Ee][Nn]|[Ss][Ee][Cc][Rr][Ee][Tt]|[Dd][Ss][Nn]|[Kk][Ee][Yy]|[Aa][Uu][Tt][Hh][Oo][Rr][Ii][Zz][Aa][Tt][Ii][Oo][Nn]|[Bb][Ee][Aa][Rr][Ee][Rr]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll])'
# Key boundaries allow digits too (e.g. "api_key1"), so numbered credentials
# are not skipped.
OUTPUT=$(mktemp "${TMPDIR:-/tmp}/hackathonkit-redacted.XXXXXX")
trap 'rm -f "$OUTPUT"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
VALUE="(\"([^\"\\\\]|\\\\.)*\"|'[^']*'|[^\"'[:space:]].*)"
sed -E \
  -e 's/([Bb][Ee][Aa][Rr][Ee][Rr][[:space:]]+)[^[:space:]]+/\1[redacted]/g' \
  -e 's/([Bb][Aa][Ss][Ii][Cc][[:space:]]+)[^[:space:]]+/\1[redacted]/g' \
  -e 's#(^|[[:space:]])[^[:space:]/@:]+:[^[:space:]/][^[:space:]]*@#\1***@#g' \
  -e 's#://[^[:space:]]*@#://***@#g' \
  -e "s/(\"[A-Za-z0-9_]*${KEY}[A-Za-z0-9_]*\"[[:space:]]*:[[:space:]]*)\"([^\"\\\\]|\\\\.)*\"/\\1\"[redacted]\"/g" \
  -e "s/([A-Za-z0-9_]*${KEY}[A-Za-z0-9_]*)([[:space:]]*[=:][[:space:]]*)${VALUE}/\\1\\3[redacted]/g" > "$OUTPUT"
cat "$OUTPUT"
