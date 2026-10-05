#!/usr/bin/env sh
# Shared credential redactor. Filters stdin -> stdout, replacing:
#   - URL userinfo (scheme://user:pass@host -> scheme://***@host). The authority
#     match stops at `/` or whitespace and takes the last '@', so a password that
#     itself contains '@', '?' or '#' is fully masked. This can over-redact a
#     benign `user@host` inside a query string; over-redaction is preferred to a
#     leak.
#   - <keyword>=<value> / <keyword>: <value> for password/pass/token/secret/
#     dsn/key (case-insensitive), with double-quoted, single-quoted or unquoted
#     values (unquoted values end at whitespace).
#   - JSON form: "keyword": "value"
#
# Fails closed: if `sed -E` is unavailable it emits a notice and no input, so
# callers never print unredacted secrets by accident.
set -e
if ! sed -E 's/a/b/' </dev/null >/dev/null 2>&1; then
  echo "[redact] sed -E unavailable; output withheld to avoid leaking credentials"
  exit 0
fi
KEY='([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss][Ss]|[Tt][Oo][Kk][Ee][Nn]|[Ss][Ee][Cc][Rr][Ee][Tt]|[Dd][Ss][Nn]|[Kk][Ee][Yy])'
VALUE="(\"[^\"]*\"|'[^']*'|[^[:space:]]+)"
sed -E \
  -e 's#://[^/[:space:]]*@#://***@#g' \
  -e "s/(\"[A-Za-z_]*${KEY}[A-Za-z_]*\"[[:space:]]*:[[:space:]]*)\"[^\"]*\"/\\1\"[redacted]\"/g" \
  -e "s/([A-Za-z_]*${KEY}[A-Za-z_]*)([[:space:]]*[=:][[:space:]]*)${VALUE}/\\1\\3[redacted]/g"
