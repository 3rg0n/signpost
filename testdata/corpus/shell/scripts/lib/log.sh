# Logging helpers. Sourced, never executed — which is why there is no shebang.

LOG_LEVEL="${LOG_LEVEL:-info}"

log_info() {
  printf '[info] %s\n' "$*"
}

log::error() {
  printf '[error] %s\n' "$*" >&2
  return 1
}

_fmt_stamp() {
  date -u '+%Y-%m-%dT%H:%M:%SZ'
}
