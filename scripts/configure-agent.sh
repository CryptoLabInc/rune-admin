#!/bin/bash
set -e

# Configure agent environment for Rune

VERSION="0.1.0"
AGENT=""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

usage() {
    cat << EOF
Rune Agent Configuration v${VERSION}

Usage: $0 --agent <agent-type>

Required:
  --agent <type>        Agent type: claude, gemini, codex, custom

Environment variables required:
  RUNEVAULT_ENDPOINT             Team Vault endpoint
  RUNEVAULT_TOKEN           Team auth token

Example:
  export RUNEVAULT_ENDPOINT="https://vault-your-team.oci.envector.io"
  export RUNEVAULT_TOKEN="evt_xxx"
  $0 --agent claude

EOF
    exit 1
}

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --agent)
            AGENT="$2"
            shift 2
            ;;
        --help|-h)
            usage
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate
if [ -z "$AGENT" ]; then
    log_error "Missing required argument: --agent"
    usage
fi

if [ -z "$RUNEVAULT_ENDPOINT" ]; then
    log_error "Environment variable RUNEVAULT_ENDPOINT not set"
    usage
fi

if [ -z "$RUNEVAULT_TOKEN" ]; then
    log_error "Environment variable RUNEVAULT_TOKEN not set"
    usage
fi

log_info "Configuring $AGENT agent..."

# Configure based on agent type
case "$AGENT" in
    claude)
        CONFIG_DIR="$HOME/.claude"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
RUNEVAULT_ENDPOINT=$RUNEVAULT_ENDPOINT
RUNEVAULT_TOKEN=$RUNEVAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Claude configured: $CONFIG_DIR/envector.env"
        ;;
    gemini)
        CONFIG_DIR="$HOME/.gemini"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
RUNEVAULT_ENDPOINT=$RUNEVAULT_ENDPOINT
RUNEVAULT_TOKEN=$RUNEVAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Gemini configured: $CONFIG_DIR/envector.env"
        ;;
    codex)
        CONFIG_DIR="$HOME/.codex"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
RUNEVAULT_ENDPOINT=$RUNEVAULT_ENDPOINT
RUNEVAULT_TOKEN=$RUNEVAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Codex configured: $CONFIG_DIR/envector.env"
        ;;
    custom)
        log_info "For custom agents, ensure these environment variables are set:"
        echo "  RUNEVAULT_ENDPOINT=$RUNEVAULT_ENDPOINT"
        echo "  RUNEVAULT_TOKEN=$RUNEVAULT_TOKEN"
        echo "  CLOUD_URL=https://api.envector.io"
        ;;
    *)
        log_error "Unknown agent: $AGENT"
        usage
        ;;
esac

echo ""
echo "${GREEN}✓ Configuration complete!${NC}"
echo ""
echo "Test your agent:"
echo "  Ask: 'Search enVector for recent decisions'"
echo ""
