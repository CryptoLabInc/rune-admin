#!/bin/bash
set -e

# Start local development Vault for testing

VERSION="0.1.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ADMIN_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Colors
GREEN="\033[0;32m"
YELLOW="\033[1;33m"
NC="\033[0m"

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

RED="\033[0;31m"
log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

DEMO_TOKEN="TOKEN-FOR-DEMONSTRATION-PURPOSES-ONLY-DO-NOT-USE-IN-PRODUCTION"

# enVector Cloud settings for auto-creating team index
export VAULT_INDEX_NAME="${VAULT_INDEX_NAME:-runecontext}"
export ENVECTOR_ENDPOINT="${ENVECTOR_ENDPOINT:-}"
export ENVECTOR_API_KEY="${ENVECTOR_API_KEY:-}"
export EMBEDDING_DIM="${EMBEDDING_DIM:-384}"

if [ -z "$ENVECTOR_ENDPOINT" ] || [ -z "$ENVECTOR_API_KEY" ]; then
    log_warn "ENVECTOR_ENDPOINT and/or ENVECTOR_API_KEY not set. Team index auto-creation will be skipped."
fi

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    log_warn "Docker not found. Install: https://docs.docker.com/get-docker/"
    exit 1
fi

# Check Docker permissions
if ! docker info &> /dev/null; then
    log_error "Cannot connect to Docker daemon. Either Docker is not running or you need permissions."
    echo "  Fix: sudo usermod -aG docker \$USER && newgrp docker"
    exit 1
fi

log_info "Starting local development Vault..."
log_warn "This is for TESTING ONLY. DO NOT use in production!"

# Try Docker Compose first (preferred)
if [ -f "$ADMIN_DIR/mcp/vault/docker-compose.yml" ]; then
    cd "$ADMIN_DIR/mcp/vault"

    # Build if image doesn't exist
    if ! docker images | grep -q "rune-vault"; then
        log_info "Building rune-vault image..."
        docker compose build vault-mcp
    fi

    log_info "Starting Vault via Docker Compose..."
    VAULT_INDEX_NAME="$VAULT_INDEX_NAME" \
    ENVECTOR_ENDPOINT="$ENVECTOR_ENDPOINT" \
    ENVECTOR_API_KEY="$ENVECTOR_API_KEY" \
    EMBEDDING_DIM="$EMBEDDING_DIM" \
    docker compose up -d vault-mcp

    # Wait for Vault to be ready
    log_info "Waiting for Vault to be ready..."
    for i in $(seq 1 30); do
        if curl -s http://localhost:50080/health > /dev/null 2>&1; then
            break
        fi
        sleep 1
    done

    if curl -s http://localhost:50080/health > /dev/null 2>&1; then
        log_info "Vault is running!"
    else
        log_warn "Vault may not be ready yet. Check: docker logs vault-mcp"
    fi

    echo ""
    echo -e "${GREEN}Local Development Vault Started!${NC}"
    echo ""
    echo -e "${YELLOW}Endpoint:${NC} http://localhost:50080"
    echo -e "${YELLOW}Token:${NC} $DEMO_TOKEN"
    echo ""
    echo "Export these for testing:"
    echo "  export RUNEVAULT_ENDPOINT=\"http://localhost:50080\""
    echo "  export RUNEVAULT_TOKEN=\"$DEMO_TOKEN\""
    echo ""
    echo "Stop with: cd mcp/vault && docker compose down"
    exit 0
fi

# Fallback: run directly with Python
log_warn "docker-compose.yml not found. Running Vault directly with Python..."

VENV_PATH="$ADMIN_DIR/.venv"
if [ ! -d "$VENV_PATH" ]; then
    log_info "Creating Python virtual environment..."
    python3 -m venv "$VENV_PATH"
    source "$VENV_PATH/bin/activate"
    log_info "Installing dependencies..."
    pip install -q -r "$ADMIN_DIR/mcp/vault/requirements.txt"
else
    log_info "Using existing virtual environment..."
    source "$VENV_PATH/bin/activate"
fi

cd "$ADMIN_DIR/mcp/vault"
log_info "Starting Vault MCP server on http://localhost:50080..."
VAULT_INDEX_NAME="$VAULT_INDEX_NAME" \
ENVECTOR_ENDPOINT="$ENVECTOR_ENDPOINT" \
ENVECTOR_API_KEY="$ENVECTOR_API_KEY" \
EMBEDDING_DIM="$EMBEDDING_DIM" \
python3 vault_mcp.py server --port 50080 --host 127.0.0.1 &
VAULT_PID=$!

sleep 3

echo ""
echo -e "${GREEN}Local Development Vault Started (Python)!${NC}"
echo ""
echo -e "${YELLOW}Endpoint:${NC} http://localhost:50080"
echo -e "${YELLOW}Token:${NC} $DEMO_TOKEN"
echo -e "${YELLOW}PID:${NC} $VAULT_PID"
echo ""
echo "Export these for testing:"
echo "  export RUNEVAULT_ENDPOINT=\"http://localhost:50080\""
echo "  export RUNEVAULT_TOKEN=\"$DEMO_TOKEN\""
echo ""
echo "Stop with: kill $VAULT_PID"
