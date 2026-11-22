# Run an external command with bounded runtime on GNU and BSD/macOS hosts.
run_with_timeout() {
  limit=$1
  shift
  if command -v timeout >/dev/null 2>&1; then
    timeout -k 5 "$limit" "$@"
  elif command -v gtimeout >/dev/null 2>&1; then
    gtimeout -k 5 "$limit" "$@"
  elif command -v python3 >/dev/null 2>&1; then
    python3 -c '
import os, signal, subprocess, sys
timeout = float(sys.argv[1])
def interrupted(signum, frame):
    raise SystemExit(128 + signum)
for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
    signal.signal(sig, interrupted)
child = subprocess.Popen(sys.argv[2:], start_new_session=True)
try:
    status = child.wait(timeout=timeout)
except subprocess.TimeoutExpired:
    sys.exit(124)
finally:
    if child.returncode is None:
        for sig in (signal.SIGINT, signal.SIGTERM, signal.SIGHUP):
            signal.signal(sig, signal.SIG_IGN)
        try:
            os.killpg(child.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        child.wait()
sys.exit(status if status >= 0 else 128 - status)
' "$limit" "$@"
  else
    echo "timeout, gtimeout or Python 3 is required for a bounded command" >&2
    return 125
  fi
}
