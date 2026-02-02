#!/bin/bash
set -e

# Start local development Vault for testing

VERSION="0.1.0"

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

# Check if Docker is installed
if ! command -v docker &> /dev/null; then
    log_warn "Docker not found. Install: https://docs.docker.com/get-docker/"
    exit 1
fi

log_info "Starting local development Vault..."
log_warn "⚠️  This is for TESTING ONLY. DO NOT use in production!"

# Check if Docker image exists
if ! docker images | grep -q "envector/vault-mcp"; then
    log_warn "Docker image 'envector/vault-mcp:latest' not found."
    log_info "Running Vault directly with Python instead..."

    # Check if virtual environment exists
    VENV_PATH=".vault_venv"
    if [ ! -d "$VENV_PATH" ]; then
        log_info "Creating Python virtual environment..."
        python3 -m venv "$VENV_PATH"
        source "$VENV_PATH/bin/activate"
        log_info "Installing dependencies..."
        pip install -q mcp pyenvector uvicorn numpy
    else
        log_info "Using existing virtual environment..."
        source "$VENV_PATH/bin/activate"
    fi

    # Run vault in background
    cd mcp/vault
    log_info "Starting Vault MCP server on http://localhost:50080..."
    python3 vault_mcp.py server --port 50080 --host 127.0.0.1 &
    VAULT_PID=$!

    sleep 3

    echo ""
    echo "${GREEN}Local Development Vault Started (Python)!${NC}"
    echo ""
    echo "${YELLOW}Endpoint:${NC} http://localhost:50080"
    echo "${YELLOW}Token:${NC} envector-team-alpha (or envector-admin-001)"
    echo "${YELLOW}PID:${NC} $VAULT_PID"
    echo ""
    echo "Export these for testing:"
    echo "  export VAULT_URL=\"http://localhost:50080\""
    echo "  export VAULT_TOKEN=\"envector-team-alpha\""
    echo ""
    echo "Stop with: kill $VAULT_PID"
    exit 0
fi

# Start Vault container (if image exists)
cd mcp/vault
docker-compose up -d

# Wait for Vault to be ready
log_info "Waiting for Vault to be ready..."
sleep 3

# Test connection
if curl -s http://localhost:50080/health > /dev/null 2>&1; then
    log_info "✓ Vault is running!"
else
    log_warn "Vault may not be ready yet. Check: docker logs vault"
fi

echo ""
echo "${GREEN}Local Development Vault Started!${NC}"
echo ""
echo "${YELLOW}Endpoint:${NC} http://localhost:50080"
echo "${YELLOW}Token:${NC} demo_token_123 (insecure, for testing only)"
echo ""
echo "Export these for testing:"
echo "  export VAULT_URL=\"http://localhost:50080\""
echo "  export VAULT_TOKEN=\"demo_token_123\""
echo ""
echo "Stop with: docker-compose -f mcp/vault/docker-compose.yml down"
