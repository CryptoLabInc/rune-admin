#!/bin/bash
set -e

# Rune-Admin Interactive Installer
# Administrator setup for Vault infrastructure deployment

VERSION="0.1.0"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo -e "\n${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}\n"
}

print_info() {
    echo -e "${GREEN}✓${NC} $1"
}

print_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
}

print_step() {
    echo -e "\n${BLUE}▸${NC} $1\n"
}

check_python() {
    if ! command -v python3 &> /dev/null; then
        print_error "Python 3 is not installed"
        echo "Please install Python 3.12 or higher:"
        echo "  - macOS: brew install python3"
        echo "  - Linux: sudo apt install python3 python3-pip"
        exit 1
    fi
    
    PYTHON_VERSION=$(python3 --version | cut -d' ' -f2)
    PYTHON_MAJOR=$(echo "$PYTHON_VERSION" | cut -d. -f1)
    PYTHON_MINOR=$(echo "$PYTHON_VERSION" | cut -d. -f2)
    if [ "$PYTHON_MAJOR" -lt 3 ] || { [ "$PYTHON_MAJOR" -eq 3 ] && [ "$PYTHON_MINOR" -lt 12 ]; }; then
        print_error "Python 3.12 or higher is required (found $PYTHON_VERSION)"
        echo "pyenvector requires Python 3.12. Please upgrade:"
        echo "  - macOS: brew install python@3.12"
        echo "  - Linux: sudo apt install python3.12 python3.12-venv"
        exit 1
    fi
    print_info "Python $PYTHON_VERSION detected"
}

setup_vault_dependencies() {
    print_step "Setting up Rune-Vault dependencies..."
    
    cd mcp/vault
    
    # Create virtual environment
    if [ ! -d ".venv" ]; then
        print_info "Creating Python virtual environment..."
        python3 -m venv .venv
    else
        print_info "Virtual environment already exists"
    fi
    
    # Activate venv
    source .venv/bin/activate
    
    # Install dependencies
    print_info "Installing Python packages (this may take a few minutes)..."
    pip install --quiet --upgrade pip
    pip install --quiet -r requirements.txt
    
    print_info "Dependencies installed successfully!"
    
    cd ../..
}

show_admin_next_steps() {
    print_header "Setup Complete! Next Steps for Admin"
    
    echo "1. Deploy Rune-Vault:"
    echo "   cd mcp/vault && docker compose up -d"
    echo "   Or deploy to cloud: cd deployment/oci && terraform apply"
    echo ""
    echo "2. Share credentials with team members (securely):"
    echo "   - Vault Endpoint (e.g., vault-YOURTEAM.oci.envector.io:50051)"
    echo "   - Vault Token (set in mcp/vault/.env)"
    echo ""
    echo "3. Team members install Rune from Claude Marketplace"
    echo ""
    echo "📚 Deployment Guide: deployment/oci/README.md"
    echo "💬 Support: https://github.com/CryptoLabInc/rune-admin/issues"
    echo ""
}

show_member_next_steps() {
    print_header "Setup Complete! Next Steps for Team Member"
    
    echo "Wait for your admin to send you:"
    echo "  1. Vault Endpoint (e.g., vault-YOURTEAM.oci.envector.io:50051)"
    echo "  2. Vault Token (e.g., evt_YOURTEAM_xxx)"
    echo ""
    echo "Once received:"
    echo "  1. Install Rune from Claude Marketplace"
    echo "  2. Configure plugin with Vault endpoint and token"
    echo "  3. Start using organizational memory"
    echo ""
    echo "📚 Configuration Guide: docs/TEAM-SETUP.md"
    echo "💬 Support: https://github.com/CryptoLabInc/rune-admin/issues"
    echo ""
}

# Main interactive installation
print_header "Rune Interactive Installer v${VERSION}"

echo "Rune is an agent-agnostic organizational memory system."
echo "It helps teams capture and retrieve context across any AI agent."
echo ""

print_step "What's your role?"
echo "1) Team Admin (will deploy Rune-Vault)"
echo "2) Team Member (will connect to existing Vault)"
echo ""
read -p "Select (1 or 2): " ROLE

case "$ROLE" in
    1)
        print_header "Admin Setup"
        
        check_python
        setup_vault_dependencies
        
        print_info "Admin setup complete!"
        show_admin_next_steps
        ;;
    2)
        print_header "Team Member Setup"
        
        echo "As a team member, you don't need to install anything locally."
        echo "Your admin will provide you with a setup package."
        echo ""
        
        print_info "No installation needed!"
        show_member_next_steps
        ;;
    *)
        print_error "Invalid selection. Please run the script again."
        exit 1
        ;;
esac

print_info "Setup complete! 🎉"
