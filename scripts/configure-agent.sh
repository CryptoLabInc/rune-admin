#!/bin/bash
set -e

# Configure agent environment for HiveMinded

VERSION="0.1.0"
AGENT=""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

usage() {
    cat << EOF
HiveMinded Agent Configuration v${VERSION}

Usage: $0 --agent <agent-type>

Required:
  --agent <type>        Agent type: claude, gemini, codex, custom

Environment variables required:
  VAULT_URL             Team Vault endpoint
  VAULT_TOKEN           Team auth token

Example:
  export VAULT_URL="https://vault-your-team.oci.envector.io"
  export VAULT_TOKEN="evt_xxx"
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

if [ -z "$VAULT_URL" ]; then
    log_error "Environment variable VAULT_URL not set"
    usage
fi

if [ -z "$VAULT_TOKEN" ]; then
    log_error "Environment variable VAULT_TOKEN not set"
    usage
fi

log_info "Configuring $AGENT agent..."

# Configure based on agent type
case "$AGENT" in
    claude)
        CONFIG_DIR="$HOME/.claude"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
VAULT_URL=$VAULT_URL
VAULT_TOKEN=$VAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Claude configured: $CONFIG_DIR/envector.env"
        ;;
    gemini)
        CONFIG_DIR="$HOME/.gemini"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
VAULT_URL=$VAULT_URL
VAULT_TOKEN=$VAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Gemini configured: $CONFIG_DIR/envector.env"
        ;;
    codex)
        CONFIG_DIR="$HOME/.codex"
        mkdir -p "$CONFIG_DIR"
        cat > "$CONFIG_DIR/envector.env" << EOF
VAULT_URL=$VAULT_URL
VAULT_TOKEN=$VAULT_TOKEN
CLOUD_URL=https://api.envector.io
EOF
        log_info "✓ Codex configured: $CONFIG_DIR/envector.env"
        ;;
    custom)
        log_info "For custom agents, ensure these environment variables are set:"
        echo "  VAULT_URL=$VAULT_URL"
        echo "  VAULT_TOKEN=$VAULT_TOKEN"
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
