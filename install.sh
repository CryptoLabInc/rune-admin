#!/bin/bash
set -e

# Rune-Vault Setup Script
# Prepares environment for deploying Rune-Vault (admins only)
# Team members don't need to run this - use onboarding package instead

VERSION="0.2.0"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_header() {
    echo ""
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}$1${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
}

log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
}

print_step() {
    echo -e "\n${BLUE}▶${NC} $1"
}print_step() {
    echo -e "\n${BLUE}▶${NC} $1"
}

check_command() {
    if command -v "$1" &> /dev/null; then
        log_info "$1 is installed"
        return 0
    else
        log_error "$1 is NOT installed"
        return 1
    fi
}

# Welcome message
print_header "Rune-Vault Setup v${VERSION}"

cat << EOF
${YELLOW}Note:${NC} This script is for ${GREEN}team administrators${NC} who will deploy Rune-Vault.

${YELLOW}Team members${NC} don't need to run this - you'll receive an onboarding
package from your admin with a ready-to-use setup script.

This will:
  1. Check system requirements (Python, Docker, Terraform)
  2. Install Python dependencies for Vault
  3. Prepare vault keys directory
  4. Show next steps for deployment

EOF

read -p "Continue with Vault setup? (y/N) " -n 1 -r
echo
if [[ ! $REPLY =~ ^[Yy]$ ]]; then
    echo "Setup cancelled."
    exit 0
fi

# Step 1: Check system requirements
print_header "Step 1: Checking System Requirements"

MISSING_DEPS=0

print_step "Checking Python..."
if check_command python3; then
    PYTHON_VERSION=$(python3 --version | awk '{print $2}')
    log_info "Python version: $PYTHON_VERSION"
    
    # Check if Python >= 3.10
    MAJOR=$(echo $PYTHON_VERSION | cut -d. -f1)
    MINOR=$(echo $PYTHON_VERSION | cut -d. -f2)
    if [ "$MAJOR" -ge 3 ] && [ "$MINOR" -ge 10 ]; then
        log_info "Python version is sufficient (>= 3.10)"
    else
        log_warn "Python 3.10+ recommended (you have $PYTHON_VERSION)"
    fi
else
    MISSING_DEPS=1
    echo "Install Python: https://www.python.org/downloads/"
fi

print_step "Checking Docker..."
if check_command docker; then
    DOCKER_VERSION=$(docker --version | awk '{print $3}' | tr -d ',')
    log_info "Docker version: $DOCKER_VERSION"
else
    MISSING_DEPS=1
    echo "Install Docker: https://docs.docker.com/get-docker/"
fi

print_step "Checking Terraform..."
if check_command terraform; then
    TERRAFORM_VERSION=$(terraform version -json | grep -o '"terraform_version":"[^"]*"' | cut -d'"' -f4)
    log_info "Terraform version: $TERRAFORM_VERSION"
else
    log_warn "Terraform not found (optional, needed for cloud deployment)"
    echo "Install Terraform: https://www.terraform.io/downloads"
fi

if [ $MISSING_DEPS -eq 1 ]; then
    echo ""
    log_error "Missing required dependencies. Please install them and run this script again."
    exit 1
fi

log_info "All required dependencies are installed!"

# Step 2: Setup Python virtual environment
print_header "Step 2: Setting Up Python Environment"

VAULT_DIR="mcp/vault"
VENV_DIR="$VAULT_DIR/.venv"

print_step "Creating virtual environment..."
if [ -d "$VENV_DIR" ]; then
    log_warn "Virtual environment already exists at $VENV_DIR"
    read -p "Recreate? (y/N) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        rm -rf "$VENV_DIR"
        python3 -m venv "$VENV_DIR"
        log_info "Virtual environment recreated"
    fi
else
    python3 -m venv "$VENV_DIR"
    log_info "Virtual environment created at $VENV_DIR"
fi

print_step "Installing Python dependencies..."
source "$VENV_DIR/bin/activate"

# Check if requirements.txt exists
if [ -f "$VAULT_DIR/requirements.txt" ]; then
    pip install --quiet --upgrade pip
    pip install --quiet -r "$VAULT_DIR/requirements.txt"
    log_info "Dependencies installed from requirements.txt"
else
    # Install core dependencies
    log_warn "requirements.txt not found, installing core packages..."
    pip install --quiet --upgrade pip
    pip install --quiet pyenvector fastmcp psutil prometheus-client
    log_info "Core dependencies installed"
fi

deactivate

# Step 3: Prepare vault keys directory
print_header "Step 3: Preparing Vault Keys"

KEYS_DIR="vault_keys"

if [ -d "$KEYS_DIR" ]; then
    log_warn "Vault keys directory already exists"
    echo "Existing keys will be used. To regenerate, delete $KEYS_DIR manually."
else
    mkdir -p "$KEYS_DIR"
    log_info "Vault keys directory created: $KEYS_DIR"
    log_warn "Keys will be generated on first Vault startup"
fi

# Step 4: Next steps
print_header "Setup Complete!"

cat << EOF
${GREEN}✓ Rune-Vault setup completed successfully!${NC}

${YELLOW}Next Steps:${NC}

${BLUE}1. Deploy Vault to Cloud (Recommended)${NC}

   Choose your cloud provider:

   ${GREEN}OCI (Oracle Cloud):${NC}
   cd deployment/oci
   terraform init
   terraform apply

   ${GREEN}AWS:${NC}
   cd deployment/aws
   terraform init
   terraform apply

   ${GREEN}GCP:${NC}
   cd deployment/gcp
   terraform init
   terraform apply

   ${YELLOW}Note:${NC} You'll need to configure terraform.tfvars with:
   - team_name
   - vault_token (generate with: openssl rand -hex 16)
   - cloud-specific credentials

${BLUE}2. Or Test Locally${NC}

   For development/testing only:
   cd mcp/vault
   ./run_vault.sh

${BLUE}3. Onboard Team Members${NC}

   Once Vault is deployed, add team members:
   ./scripts/add-team-member.sh

   This generates a setup package they can run on their machine.

${YELLOW}Documentation:${NC}
- Cloud Deployment: deployment/oci/README.md (or aws/gcp)
- Team Setup: docs/TEAM-SETUP.md
- Agent Configuration: CLAUDE_SETUP.md

${YELLOW}Support:${NC}
- Issues: https://github.com/CryptoLabInc/rune/issues
- Docs: https://docs.envector.io

EOF
