#!/usr/bin/env sh
# Shared credential redactor. Filters stdin -> stdout, replacing:
#   - URL userinfo (scheme://user:pass@host -> scheme://***@host)
#   - <keyword>=<value> / <keyword>: <value> for password/pass/token/secret/
#     dsn/key (case-insensitive), including quoted values.
#
# Fails closed: if `sed -E` is unavailable it emits a notice and no input, so
# callers never print unredacted secrets by accident.
set -e
if ! sed -E 's/a/b/' </dev/null >/dev/null 2>&1; then
  echo "[redact] sed -E unavailable; output withheld to avoid leaking credentials"
  exit 0
fi
sed -E \
  -e 's#://[^/@ ]+@#://***@#g' \
  -e 's/([A-Za-z_]*([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss][Ss]|[Tt][Oo][Kk][Ee][Nn]|[Ss][Ee][Cc][Rr][Ee][Tt]|[Dd][Ss][Nn]|[Kk][Ee][Yy])[A-Za-z_]*)([=: ]+)("[^"]*"|[^ " ]+)/\1\3[redacted]/g'
