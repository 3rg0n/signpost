# Retries a command with a fixed delay.

readonly RETRY_DELAY=2

retry() {
  local attempts="$1"
  shift
  local n=0
  while [ "$n" -lt "$attempts" ]; do
    if "$@"; then
      return 0
    fi
    n=$((n + 1))
    sleep "$RETRY_DELAY"
  done
  return 1
}
