#!/bin/bash
set -e

# HiveMinded Agent-Agnostic Installer
# Installs skills for Claude, Gemini, Codex, or custom agents

VERSION="0.1.0"
AGENT=""
INSTALL_DIR=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

usage() {
    cat << EOF
HiveMinded Installer v${VERSION}

Usage: $0 --agent <agent-type> [options]

Required:
  --agent <type>        Agent type: claude, gemini, codex, custom

Optional:
  --install-dir <path>  Custom installation directory
  --help                Show this help message

Examples:
  $0 --agent claude
  $0 --agent gemini --install-dir ~/.config/gemini/skills
  $0 --agent custom --install-dir /path/to/agent/skills

EOF
    exit 1
}

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

detect_install_dir() {
    case "$AGENT" in
        claude)
            # Check for Claude Code (VS Code extension)
            if [ -d "$HOME/.vscode/extensions" ]; then
                INSTALL_DIR="$HOME/.claude/skills"
            elif [ -d "$HOME/Library/Application Support/Claude" ]; then
                # Claude Desktop on macOS
                INSTALL_DIR="$HOME/Library/Application Support/Claude/skills"
            else
                INSTALL_DIR="$HOME/.claude/skills"
            fi
            ;;
        gemini)
            INSTALL_DIR="$HOME/.gemini/skills"
            ;;
        codex)
            INSTALL_DIR="$HOME/.codex/skills"
            ;;
        custom)
            if [ -z "$INSTALL_DIR" ]; then
                log_error "Custom agent requires --install-dir"
                exit 1
            fi
            ;;
        *)
            log_error "Unknown agent type: $AGENT"
            usage
            ;;
    esac
}

install_skills() {
    log_info "Installing HiveMinded skills to: $INSTALL_DIR"
    
    # Create installation directory
    mkdir -p "$INSTALL_DIR"
    
    # Copy skills
    log_info "Installing enVector skill..."
    cp -r skills/envector "$INSTALL_DIR/"
    
    log_info "Skills installed successfully!"
    log_info "Location: $INSTALL_DIR/envector"
}

create_config() {
    log_info "Creating configuration..."
    
    # Create agent-specific config
    case "$AGENT" in
        claude)
            cat > "$INSTALL_DIR/../config.json" << 'EOF'
{
  "skills": {
    "envector": {
      "enabled": true,
      "mcp_servers": {
        "vault": {
          "url": "${VAULT_URL}",
          "token": "${VAULT_TOKEN}"
        }
      }
    }
  }
}
EOF
            ;;
        gemini|codex)
            cat > "$INSTALL_DIR/../config.json" << 'EOF'
{
  "skills": {
    "envector": {
      "enabled": true,
      "mcp_servers": {
        "vault": {
          "url": "${VAULT_URL}",
          "token": "${VAULT_TOKEN}"
        }
      }
    }
  }
}
EOF
            ;;
    esac
    
    log_info "Config created at: $INSTALL_DIR/../config.json"
}

show_next_steps() {
    cat << EOF

${GREEN}✓ HiveMinded installed successfully!${NC}

Next steps:

1. Deploy team Vault (or use local dev):
   ${YELLOW}./scripts/deploy-vault.sh --provider oci --team-name your-team${NC}
   
   Or for local development:
   ${YELLOW}./scripts/vault-dev.sh${NC}

2. Configure environment:
   ${YELLOW}export VAULT_URL="https://vault-your-team.oci.envector.io"${NC}
   ${YELLOW}export VAULT_TOKEN="evt_xxx"${NC}

3. Configure your agent:
   ${YELLOW}./scripts/configure-agent.sh --agent $AGENT${NC}

4. Test the installation:
   Open your agent and try:
   ${YELLOW}"Search enVector for recent decisions"${NC}

Documentation: docs/AGENT-INTEGRATION.md
Support: https://github.com/zotanika/HiveMinded/issues

EOF
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --agent)
            AGENT="$2"
            shift 2
            ;;
        --install-dir)
            INSTALL_DIR="$2"
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

# Validate required arguments
if [ -z "$AGENT" ]; then
    log_error "Missing required argument: --agent"
    usage
fi

# Main installation
log_info "HiveMinded Installer v${VERSION}"
log_info "Agent: $AGENT"

detect_install_dir
install_skills
create_config
show_next_steps

log_info "Installation complete!"
