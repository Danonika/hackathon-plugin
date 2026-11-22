#!/usr/bin/env sh
# Test the actual publication policy, including a host without a Python exchange primitive.
set -eu
scripts=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$scripts/publish-mirror.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
live=$work/live
stage=$work/stage
real_path=$PATH
mkdir "$work/tools"
for name in sh mkdir find cmp diff stat mv rm readlink; do
  ln -s "$(command -v "$name")" "$work/tools/$name"
done
stage_value() {
  rm -rf "$stage"
  mkdir "$stage"
  printf '%s\n' "$1" > "$stage/content"
}
inode() { stat -c %i "$1" 2>/dev/null || stat -f %i "$1"; }
PATH=$work/tools
export PATH
unset HACKATHONKIT_SYNC_OFFLINE
stage_value one
publish_mirror "$stage" "$live"
before=$(inode "$live")
stage_value one
publish_mirror "$stage" "$live"
[ "$(inode "$live")" = "$before" ]
stage_value two
if publish_mirror "$stage" "$live" 2>"$work/diagnostic"; then
  echo "changed existing mirror published without exchange" >&2
  exit 1
fi
[ "$(inode "$live")" = "$before" ]
IFS= read -r value < "$live/content"
[ "$value" = one ]
HACKATHONKIT_SYNC_OFFLINE=1 publish_mirror "$stage" "$live"
IFS= read -r value < "$live/content"
[ "$value" = two ]
PATH=$real_path
export PATH
# Native exchange leaves the previous directory at stage until caller cleanup.
stage_value three
if publish_mirror "$stage" "$live" 2>"$work/native-error"; then
  grep -qx three "$live/content"
  grep -qx two "$stage/content"
  echo "[publish-mirror-test] native exchange verified"
elif grep -q 'atomic exchange unavailable' "$work/native-error"; then
  echo "[publish-mirror-test] native exchange unavailable on this filesystem"
else
  cat "$work/native-error" >&2
  exit 1
fi
echo "[publish-mirror-test] first install, unchanged, protected update and offline replacement passed"
