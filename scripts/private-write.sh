# Restrict executable paths to the owner (or root), preserving read/execute bits.
private_write() {
  path=$1
  if [ -L "$path" ] || { [ ! -f "$path" ] && [ ! -d "$path" ]; }; then
    echo "refusing linked or non-regular path: $path" >&2
    return 1
  fi
  file_owner=$(stat -c %u "$path" 2>/dev/null || stat -f %u "$path") || return 1
  if [ "$file_owner" != "$(id -u)" ] && [ "$file_owner" != 0 ]; then
    echo "unsupported path owner: $path" >&2
    return 1
  fi
  mode=$(stat -c %a "$path" 2>/dev/null || stat -f %Lp "$path") || return 1
  if [ $((0$mode & 022)) -ne 0 ]; then chmod go-w "$path"; fi
}
