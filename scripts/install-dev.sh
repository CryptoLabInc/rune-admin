#!/usr/bin/env bash
#
# Local testing wrapper for install.sh.
#
# Uses the locally built binary instead of downloading from GitHub.
# Skips cosign verification. Useful for testing the installer flow without
# a published release.
#
# Usage:
#   mise run go:build                         # build the binary first
#   sudo bash scripts/install-dev.sh          # full install to /opt/runevault
#   bash scripts/install-dev.sh --prefix /tmp/vault-test  # rootless local test

set -euo pipefail

REPO_ROOT=$(cd "$(dirname "$0")/.." && pwd)
LOCAL_BINARY="${REPO_ROOT}/vault/bin/runevault"

[[ -x "$LOCAL_BINARY" ]] || {
  printf 'ERROR: Binary not found at %s\n' "$LOCAL_BINARY" >&2
  printf '  Run: mise run go:build\n' >&2
  exit 1
}

# Parse --prefix for rootless local testing; pass all other args to install.sh
PREFIX=""
PASSTHROUGH_ARGS=()
while [[ $# -gt 0 ]]; do
  case $1 in
    --prefix) PREFIX="$2"; shift 2 ;;
    *)        PASSTHROUGH_ARGS+=("$1"); shift ;;
  esac
done

export RUNEVAULT_LOCAL_BINARY="$LOCAL_BINARY"
export RUNEVAULT_SKIP_VERIFY=1

if [[ -n "$PREFIX" ]]; then
  export RUNEVAULT_INSTALL_PREFIX="$PREFIX"
  export RUNEVAULT_BINARY_PATH="${PREFIX}/runevault"
  export RUNEVAULT_SKIP_SERVICE=1
fi

# Dev defaults for non-interactive installs
export RUNEVAULT_TEAM_NAME="${RUNEVAULT_TEAM_NAME:-dev-team}"
export RUNEVAULT_ENVECTOR_ENDPOINT="${RUNEVAULT_ENVECTOR_ENDPOINT:-https://envector.example.com}"
export RUNEVAULT_ENVECTOR_API_KEY="${RUNEVAULT_ENVECTOR_API_KEY:-dev-api-key-placeholder}"

exec bash "${REPO_ROOT}/install.sh" "${PASSTHROUGH_ARGS[@]+"${PASSTHROUGH_ARGS[@]}"}"
