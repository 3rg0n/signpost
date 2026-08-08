#!/usr/bin/env bash
# Deploys the corpus service to a named environment.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# The anchored form, which is what a script runnable from any directory has to write.
source "$SCRIPT_DIR/lib/log.sh"
. "$(dirname "$0")/lib/retry.sh"

# The near-miss: one letter from lib/log.sh, and a resolver matching a directory or a
# prefix rather than a file reaches the real one.
source "$SCRIPT_DIR/lib/logs.sh"

# A path assembled from variables. Nothing can name the file, so no edge is drawn and no
# gap is reported either — there is no specifier to report.
source "$CONFIG_DIR/$DEPLOY_ENV.sh"

readonly DEFAULT_REGION="us-east-1"
export DEPLOY_ENV="${DEPLOY_ENV:-staging}"

# Pushes the built image to the registry.
push_image() {
  local tag="$1"
  log_info "pushing $tag"
  retry 3 docker push "$tag"
}

# Internal: not part of the interface a caller sees.
_cleanup() {
  rm -rf /tmp/corpus-push
}

main() {
  push_image "$1"
  _cleanup
}

main "$@"
