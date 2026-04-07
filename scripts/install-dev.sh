#!/bin/bash
# Rune-Vault Interactive Server Setup — Local Development Version
# Uses files from the local working tree instead of downloading from GitHub.
# Usage: sudo bash scripts/install-dev.sh
#
# Build the Docker image first:
#   mise run build dev

set -euo pipefail

# ─── Root privilege check ─────────────────────────────────────────────────────

if [ "$(id -u)" -ne 0 ]; then
    echo "Error: This script must be run as root. Use: sudo bash scripts/install-dev.sh"
    exit 1
fi

# ─── Resolve repo root ──────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

if [ ! -f "$REPO_ROOT/install.sh" ]; then
    echo "Error: Cannot find repo root. Run from the repository directory:"
    echo "  sudo bash scripts/install-dev.sh"
    exit 1
fi

# ─── Resolve Docker tag from git state ───────────────────────────────────────

DOCKER_IMAGE="ghcr.io/cryptolabinc/rune-vault"
GIT_BRANCH="$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD | sed 's|/|-|g')"
GIT_COMMIT="$(git -C "$REPO_ROOT" rev-parse --short HEAD)"
DOCKER_TAG="${GIT_BRANCH}-${GIT_COMMIT}"
DEFAULT_INSTALL_DIR="$HOME/rune-vault-dev"
VAULT_PUBLIC_IP=""
CSP_CA_CERT_LOCAL=""

# ─── Colors & output helpers ─────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
BOLD='\033[1m'
NC='\033[0m'

print_header() {
    echo -e "\n${BLUE}================================================${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}================================================${NC}\n"
}

print_info()  { echo -e "${GREEN}✓${NC} $1"; }
print_warn()  { echo -e "${YELLOW}⚠${NC} $1"; }
print_error() { echo -e "${RED}✗${NC} $1"; }
print_step()  { echo -e "\n${BOLD}▸ $1${NC}\n"; }

# ─── Cleanup trap ─────────────────────────────────────────────────────────────

CLEANUP_DIR=""
cleanup() {
    printf '\033[?25h' >&2 2>/dev/null || true
    if [ -n "$CLEANUP_DIR" ] && [ -d "$CLEANUP_DIR" ]; then
        rm -rf "$CLEANUP_DIR"
    fi
}
trap cleanup EXIT

# ─── Prompt helper ────────────────────────────────────────────────────────────

prompt() {
    local varname="$1" message="$2" default="${3:-}"
    if [ -n "$default" ]; then
        printf "${BOLD}%s${NC} [%s]: " "$message" "$default" >&2
    else
        printf "${BOLD}%s${NC}: " "$message" >&2
    fi
    local value
    read -r value
    value="${value:-$default}"
    eval "$varname=\"\$value\""
}

prompt_yn() {
    local message="$1" default="${2:-y}"
    local value
    if [ "$default" = "y" ]; then
        printf "${BOLD}%s${NC} [Y/n]: " "$message" >&2
    else
        printf "${BOLD}%s${NC} [y/N]: " "$message" >&2
    fi
    read -r value
    value="${value:-$default}"
    case "$value" in
        [Yy]*) return 0 ;;
        *) return 1 ;;
    esac
}

# ─── Arrow-key menu selector ────────────────────────────────────────────────

select_menu() {
    local options=("$@")
    local count=${#options[@]}
    local _sel=0

    # Fallback: plain number input when terminal is dumb or unset
    if [ -z "${TERM:-}" ] || [ "$TERM" = "dumb" ]; then
        local i
        for i in "${!options[@]}"; do
            printf "  %d) %s\n" "$((i + 1))" "${options[$i]}" >&2
        done
        echo "" >&2
        local choice
        printf "${BOLD}Select${NC} [1]: " >&2
        read -r choice
        choice="${choice:-1}"
        if [ "$choice" -ge 1 ] 2>/dev/null && [ "$choice" -le "$count" ] 2>/dev/null; then
            echo "$((choice - 1))"
        else
            print_error "Invalid selection."; exit 1
        fi
        return
    fi

    # ── Draw the menu ──
    _draw_menu() {
        local i
        for i in "${!options[@]}"; do
            if [ "$i" -eq "$_sel" ]; then
                printf "  ${GREEN}${BOLD}> %s${NC}\n" "${options[$i]}" >&2
            else
                printf "    %s\n" "${options[$i]}" >&2
            fi
        done
    }

    # ── Move cursor up to redraw ──
    _erase_menu() {
        local i
        for (( i = 0; i < count; i++ )); do
            printf '\033[1A\033[2K' >&2
        done
    }

    printf '\033[?25l' >&2          # hide cursor
    printf "  ${BOLD}↑↓ move  Enter confirm${NC}\n" >&2
    _draw_menu

    while true; do
        local key=""
        IFS= read -rsn1 key
        if [ "$key" = $'\x1b' ]; then
            local seq=""
            IFS= read -rsn2 -t 1 seq || true
            case "$seq" in
                '[A') # Up arrow
                    if [ "$_sel" -gt 0 ]; then
                        _sel=$((_sel - 1))
                    else
                        _sel=$((count - 1))
                    fi
                    ;;
                '[B') # Down arrow
                    if [ "$_sel" -lt $((count - 1)) ]; then
                        _sel=$((_sel + 1))
                    else
                        _sel=0
                    fi
                    ;;
            esac
            _erase_menu
            _draw_menu
        elif [ "$key" = "" ]; then
            # Enter key
            break
        elif [ "$key" -ge 1 ] 2>/dev/null && [ "$key" -le "$count" ] 2>/dev/null; then
            # Number key direct jump
            _sel=$((key - 1))
            _erase_menu
            _draw_menu
        fi
    done

    printf '\033[?25h' >&2          # show cursor

    echo "$_sel"
}

# ─── Local file copy helper (replaces download_file) ────────────────────────

copy_local_file() {
    local src="$1" dest="$2"
    if [ ! -f "$src" ]; then
        print_error "Local file not found: $src"
        exit 1
    fi
    cp "$src" "$dest"
}

# ─── Prerequisite checks ─────────────────────────────────────────────────────

check_command() {
    local cmd="$1" install_hint="$2"
    if ! command -v "$cmd" &>/dev/null; then
        print_error "'$cmd' is not installed."
        echo "  Install: $install_hint"
        return 1
    fi
    print_info "$cmd found"
    return 0
}

check_prerequisites_local() {
    print_step "Checking prerequisites..."
    local missing=0
    check_command mise "https://mise.jdx.dev" || missing=1
    check_command docker "https://docs.docker.com/get-docker/" || missing=1
    check_command openssl "apt install openssl / brew install openssl" || missing=1

    # docker compose (v2 plugin)
    if ! docker compose version &>/dev/null 2>&1; then
        print_error "'docker compose' (v2 plugin) is not available."
        echo "  Install: https://docs.docker.com/compose/install/"
        missing=1
    else
        print_info "docker compose found"
    fi

    if [ "$missing" -eq 1 ]; then
        echo ""
        print_error "Please install the missing prerequisites and re-run."
        exit 1
    fi

    # Check Docker daemon
    if ! docker info &>/dev/null 2>&1; then
        print_error "Cannot connect to Docker daemon. Is Docker running?"
        echo "  Fix: systemctl start docker"
        exit 1
    fi

    (cd "$REPO_ROOT" && mise trust)
}

check_prerequisites_csp() {
    local provider="$1"
    print_step "Checking prerequisites..."

    local missing=0
    check_command mise "https://mise.jdx.dev" || missing=1
    check_command terraform "https://developer.hashicorp.com/terraform/install" || missing=1
    check_command openssl "apt install openssl / brew install openssl" || missing=1
    check_command gh "https://cli.github.com/" || missing=1
    check_command docker "https://docs.docker.com/get-docker/" || missing=1

    case "$provider" in
        aws) check_command aws "https://aws.amazon.com/cli/" || missing=1 ;;
        gcp) check_command gcloud "https://cloud.google.com/sdk/docs/install" || missing=1 ;;
        oci) check_command oci "https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm" || missing=1 ;;
    esac

    if [ "$missing" -eq 1 ]; then
        echo ""
        print_error "Please install the missing prerequisites and re-run."
        exit 1
    fi

    (cd "$REPO_ROOT" && mise trust)
}

# ─── Interactive prompts ─────────────────────────────────────────────────────

choose_deploy_target() {
    print_step "Select deployment target"
    local options=("Local (This machine)" "AWS (requires GHCR access)" "GCP (requires GHCR access)" "OCI (requires GHCR access)")
    local targets=("local" "aws" "gcp" "oci")
    local selected
    selected=$(select_menu "${options[@]}")
    DEPLOY_TARGET="${targets[$selected]}"
    print_info "Deployment target: ${DEPLOY_TARGET}"
}

prompt_install_dir() {
    print_step "Installation directory"
    local default_dir="$DEFAULT_INSTALL_DIR"
    if [ "$DEPLOY_TARGET" != "local" ]; then
        default_dir="$HOME/rune-vault-${DEPLOY_TARGET}"
        echo "  Terraform files, state, and SSH keys are stored here."
        echo "  Keep this directory to manage (update/destroy) your deployment."
        echo ""
    fi
    prompt INSTALL_DIR "Directory" "$default_dir"
}

prompt_tls_mode() {
    print_step "TLS configuration"
    local options=("Generate self-signed certificate" "No TLS (not recommended)")
    local modes=("self-signed" "none")
    local selected
    selected=$(select_menu "${options[@]}")
    TLS_MODE="${modes[$selected]}"

    if [ "$TLS_MODE" = "self-signed" ]; then
        echo ""
        prompt TLS_HOSTNAME "Domain name for the certificate (leave empty if none)" ""
    fi

    if [ "$TLS_MODE" = "none" ]; then
        print_warn "Running without TLS. gRPC traffic will be unencrypted."
        print_warn "This is NOT recommended for production."
    fi

    print_info "TLS mode: ${TLS_MODE}"
}

prompt_envector_config() {
    print_step "enVector Cloud configuration"
    echo "  Create your enVector cluster at https://envector.io before proceeding."
    echo "  You will need the endpoint URL and API key from the dashboard."
    echo "  Index name is used to store and retrieve your team's organizational memory."
    echo ""
    prompt ENVECTOR_ENDPOINT "enVector endpoint (e.g. cluster-id.clusters.envector.io)"
    prompt ENVECTOR_API_KEY "enVector API key (e.g. aBcDE_12345_xxxxx)"
    prompt VAULT_INDEX_NAME "Index name" "runecontext"

    if [ -z "$ENVECTOR_ENDPOINT" ] || [ -z "$ENVECTOR_API_KEY" ]; then
        print_error "enVector endpoint and API key are required."
        exit 1
    fi
    if [ -z "$VAULT_INDEX_NAME" ]; then
        print_error "Index name is required."
        exit 1
    fi
    print_info "enVector endpoint: ${ENVECTOR_ENDPOINT}"
}

prompt_csp_config() {
    prompt TEAM_NAME "Team name (used for resource naming)" "default"

    case "$DEPLOY_TARGET" in
        aws)
            prompt CSP_REGION "AWS region" "us-east-1"
            ;;
        gcp)
            prompt CSP_REGION "GCP region" "us-central1"
            prompt GCP_PROJECT_ID "GCP project ID"
            if [ -z "$GCP_PROJECT_ID" ]; then
                print_error "GCP project ID is required."; exit 1
            fi
            ;;
        oci)
            prompt CSP_REGION "OCI region" "us-ashburn-1"
            prompt OCI_COMPARTMENT_ID "OCI compartment OCID"
            if [ -z "$OCI_COMPARTMENT_ID" ]; then
                print_error "OCI compartment OCID is required."; exit 1
            fi
            ;;
    esac
}

generate_team_secret() {
    VAULT_TEAM_SECRET_VALUE="evt_$(openssl rand -hex 32)"
    print_info "Team secret generated."
}

generate_config_files() {
    local dir="$1"

    cat > "$dir/vault-roles.yml" <<'ROLESEOF'
roles:
  admin:
    scope: [get_public_key, decrypt_scores, decrypt_metadata, manage_tokens]
    top_k: 50
    rate_limit: 150/60s
  agent:
    scope: [get_public_key, decrypt_scores, decrypt_metadata]
    top_k: 10
    rate_limit: 30/60s
ROLESEOF

    cat > "$dir/vault-tokens.yml" <<'TOKENSEOF'
tokens: []
TOKENSEOF

    chmod 600 "$dir/vault-roles.yml" "$dir/vault-tokens.yml"
    print_info "Token/role config files created."
}

setup_runevault_alias() {
    if [ -z "${SUDO_USER:-}" ]; then
        return
    fi

    # Add user to docker group
    if command -v usermod >/dev/null 2>&1; then
        usermod -aG docker "$SUDO_USER" 2>/dev/null || true
    fi

    # Detect shell config
    local user_home
    user_home="$(eval echo ~"$SUDO_USER")"
    local shell_rc=""
    if [ -f "$user_home/.zshrc" ]; then
        shell_rc="$user_home/.zshrc"
    elif [ -f "$user_home/.bashrc" ]; then
        shell_rc="$user_home/.bashrc"
    fi

    if [ -n "$shell_rc" ]; then
        if ! grep -q 'alias runevault=' "$shell_rc" 2>/dev/null; then
            echo '' >> "$shell_rc"
            echo '# Rune-Vault admin CLI' >> "$shell_rc"
            echo 'alias runevault="docker exec -it rune-vault python3 /app/vault_admin_cli.py"' >> "$shell_rc"
            print_info "runevault alias added to ${shell_rc}"
            print_warn "Run 'exec \$SHELL' to reload your shell and enable the runevault command."
        fi
    fi
}

# ─── Confirmation summary ────────────────────────────────────────────────────

show_confirmation() {
    print_header "Configuration Summary (DEV — local build)"
    echo "  Deployment target : ${DEPLOY_TARGET}"
    echo "  Install directory : ${INSTALL_DIR}"
    echo "  Docker image      : ${DOCKER_IMAGE}:${DOCKER_TAG} (local)"
    echo "  Repo root         : ${REPO_ROOT}"
    echo "  TLS mode          : ${TLS_MODE}"
    [ -n "${TLS_HOSTNAME:-}" ] && echo "  TLS domain        : ${TLS_HOSTNAME}"
    echo "  Team secret       : (auto-generated in .env)"
    echo "  enVector endpoint : ${ENVECTOR_ENDPOINT}"
    echo "  Index name        : ${VAULT_INDEX_NAME}"
    if [ "$DEPLOY_TARGET" != "local" ]; then
        echo "  Team name         : ${TEAM_NAME}"
        echo "  Region            : ${CSP_REGION}"
        [ "${DEPLOY_TARGET}" = "gcp" ] && echo "  GCP project       : ${GCP_PROJECT_ID}"
        [ "${DEPLOY_TARGET}" = "oci" ] && echo "  OCI compartment   : ${OCI_COMPARTMENT_ID}"
    fi
    echo ""

    if ! prompt_yn "Proceed with deployment?"; then
        print_warn "Aborted."
        exit 0
    fi
}

# ─── TLS handling ─────────────────────────────────────────────────────────────

setup_tls() {
    local certs_dir="$INSTALL_DIR/certs"
    mkdir -p "$certs_dir"

    case "$TLS_MODE" in
        self-signed)
            print_step "Generating self-signed certificates..."
            copy_local_file "$REPO_ROOT/scripts/generate-certs.sh" "$certs_dir/generate-certs.sh"
            chmod +x "$certs_dir/generate-certs.sh"
            (cd "$certs_dir" && bash generate-certs.sh . "${TLS_HOSTNAME:-localhost}")
            TLS_CERT_PATH="$certs_dir/server.pem"
            TLS_KEY_PATH="$certs_dir/server.key"
            TLS_CA_PATH="$certs_dir/ca.pem"
            print_info "Self-signed certificates generated in ${certs_dir}/"
            ;;
        none)
            print_warn "Skipping TLS setup."
            TLS_CERT_PATH=""
            TLS_KEY_PATH=""
            TLS_CA_PATH=""
            ;;
    esac
}

# ─── Generate .env file ──────────────────────────────────────────────────────

generate_env_file() {
    local env_file="$INSTALL_DIR/.env"

    cat > "$env_file" <<ENVEOF
# Rune-Vault configuration — generated by install-dev.sh
VAULT_TEAM_SECRET=${VAULT_TEAM_SECRET_VALUE}
VAULT_INDEX_NAME=${VAULT_INDEX_NAME}
ENVECTOR_ENDPOINT=${ENVECTOR_ENDPOINT}
ENVECTOR_API_KEY=${ENVECTOR_API_KEY}
EMBEDDING_DIM=768
RUNE_VAULT_TAG=${DOCKER_TAG}
ENVEOF

    if [ "$TLS_MODE" = "none" ]; then
        echo "VAULT_TLS_DISABLE=true" >> "$env_file"
    else
        cat >> "$env_file" <<TLSEOF
VAULT_TLS_CERT=/app/certs/server.pem
VAULT_TLS_KEY=/app/certs/server.key
TLSEOF
    fi

    chmod 600 "$env_file"
    print_info ".env file created (mode 600)."
}

# ─── Local deployment ─────────────────────────────────────────────────────────

deploy_local() {
    print_header "Deploying Rune-Vault (Local — DEV)"

    # Clean up existing installation
    if [ -d "$INSTALL_DIR" ]; then
        print_warn "Existing installation found at ${INSTALL_DIR}"
        if prompt_yn "Remove existing installation and reinstall?" "n"; then
            print_step "Removing existing installation..."
            (cd "$INSTALL_DIR" && docker compose down -v 2>/dev/null) || true
            rm -rf "$INSTALL_DIR"
            print_info "Previous installation removed."
        else
            print_warn "Aborted."
            exit 0
        fi
    fi
    # Clean up orphaned container/network/volume
    local project
    project="$(basename "$INSTALL_DIR")"
    if docker container inspect rune-vault &>/dev/null; then
        print_step "Removing existing rune-vault container..."
        docker rm -f rune-vault >/dev/null 2>&1 || true
        print_info "Container removed."
    fi
    docker network rm "${project}_vault-net" >/dev/null 2>&1 || true
    docker volume rm "${project}_vault-keys" >/dev/null 2>&1 || true

    # Create directory structure
    mkdir -p "$INSTALL_DIR"/{certs,backups,logs}
    print_info "Directory structure created: ${INSTALL_DIR}/"

    # Copy docker-compose.yml from local repo
    print_step "Copying docker-compose.yml from local repo..."
    copy_local_file "$REPO_ROOT/vault/docker-compose.yml" "$INSTALL_DIR/docker-compose.yml"
    # Pin image to the local build tag
    sed -i.bak "s|image:.*rune-vault:.*|image: ${DOCKER_IMAGE}:${DOCKER_TAG}|" "$INSTALL_DIR/docker-compose.yml"
    rm -f "$INSTALL_DIR/docker-compose.yml.bak"
    print_info "docker-compose.yml copied."

    # TLS
    setup_tls

    # Generate .env and config files
    generate_env_file
    generate_config_files "$INSTALL_DIR"

    # Restore ownership to the invoking user (files were created as root via sudo)
    if [ -n "${SUDO_USER:-}" ]; then
        chown -R "$SUDO_USER" "$INSTALL_DIR"
    fi

    # Build Docker image from local source
    print_step "Building Docker image (tag: ${DOCKER_TAG})..."
    (cd "$REPO_ROOT" && mise run build "${DOCKER_TAG}")
    print_info "Image built: ${DOCKER_IMAGE}:${DOCKER_TAG}"

    # Start container
    print_step "Starting Rune-Vault..."
    (cd "$INSTALL_DIR" && docker compose up -d)
    print_info "Container started."

    # Health check
    print_step "Waiting for Vault to become healthy..."
    local elapsed=0
    local timeout=60
    while [ $elapsed -lt $timeout ]; do
        if docker exec rune-vault curl -sf http://localhost:8081/health 2>/dev/null; then
            print_info "Vault is healthy!"

            # Set up runevault alias for admin CLI
            setup_runevault_alias
            return 0
        fi
        sleep 2
        elapsed=$((elapsed + 2))
        printf "."
    done

    echo ""
    print_error "Vault did not become healthy within ${timeout}s."
    print_warn "Container logs:"
    docker logs rune-vault 2>&1 | tail -30
    exit 1
}

# ─── CSP deployment ───────────────────────────────────────────────────────────

deploy_csp() {
    local provider="$DEPLOY_TARGET"
    print_header "Deploying Rune-Vault (${provider} — DEV)"

    local tf_dir="$INSTALL_DIR/deployment"
    mkdir -p "$tf_dir"
    # Ensure the original user owns the deployment directory for terraform
    if [ -n "${SUDO_USER:-}" ]; then
        chown -R "$SUDO_USER" "$INSTALL_DIR"
    fi

    # Build and push Docker image to GHCR (remote servers pull from registry)
    # Requires GHCR push access to the CryptoLabInc organization.
    print_step "Building and pushing Docker image to GHCR..."
    echo "  CSP deployments pull the image from GHCR, so a push is required."
    echo "  This requires GHCR push access to the CryptoLabInc organization."
    echo ""
    if ! gh auth status &>/dev/null; then
        print_error "GitHub CLI not authenticated. Run: gh auth login"
        exit 1
    fi
    (cd "$REPO_ROOT" && mise run push "${DOCKER_TAG}")
    print_info "Image pushed: ${DOCKER_IMAGE}:${DOCKER_TAG}"

    # Copy Terraform files from local repo
    print_step "Copying Terraform configuration from local repo..."
    copy_local_file "$REPO_ROOT/deployment/${provider}/main.tf" "$tf_dir/main.tf"
    if [ "$provider" = "aws" ]; then
        copy_local_file "$REPO_ROOT/deployment/${provider}/cloud-init.yaml" "$tf_dir/cloud-init.yaml"
        sed -i.bak "s|image:.*rune-vault:.*|image: ${DOCKER_IMAGE}:${DOCKER_TAG}|" "$tf_dir/cloud-init.yaml"
        rm -f "$tf_dir/cloud-init.yaml.bak"
    else
        copy_local_file "$REPO_ROOT/deployment/${provider}/startup-script.sh" "$tf_dir/startup-script.sh"
        sed -i.bak "s|image:.*rune-vault:.*|image: ${DOCKER_IMAGE}:${DOCKER_TAG}|" "$tf_dir/startup-script.sh"
        rm -f "$tf_dir/startup-script.sh.bak"
    fi
    print_info "Terraform files copied."

    # Generate SSH key pair for EC2 access
    local ssh_key_path="$INSTALL_DIR/ssh_key"
    if [ ! -f "$ssh_key_path" ]; then
        print_step "Generating SSH key pair..."
        ssh-keygen -t ed25519 -f "$ssh_key_path" -N "" -q
        chmod 600 "$ssh_key_path"
        chmod 644 "${ssh_key_path}.pub"
        print_info "SSH key generated: ${ssh_key_path}"
    fi
    local public_key
    public_key=$(cat "${ssh_key_path}.pub")

    # Generate terraform.tfvars (use printf to avoid heredoc escaping issues)
    print_step "Generating terraform.tfvars..."
    escape_tf() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }
    {
        printf 'team_secret        = "%s"\n' "$(escape_tf "$VAULT_TEAM_SECRET_VALUE")"
        printf 'team_name          = "%s"\n' "$(escape_tf "$TEAM_NAME")"
        printf 'region             = "%s"\n' "$(escape_tf "$CSP_REGION")"
        printf 'tls_mode           = "%s"\n' "$(escape_tf "$TLS_MODE")"
        printf 'tls_hostname       = "%s"\n' "$(escape_tf "${TLS_HOSTNAME:-}")"
        printf 'envector_endpoint  = "%s"\n' "$(escape_tf "$ENVECTOR_ENDPOINT")"
        printf 'envector_api_key   = "%s"\n' "$(escape_tf "$ENVECTOR_API_KEY")"
        printf 'vault_index_name   = "%s"\n' "$(escape_tf "$VAULT_INDEX_NAME")"
        printf 'public_key         = "%s"\n' "$(escape_tf "$public_key")"
        case "$provider" in
            gcp) printf 'project_id         = "%s"\n' "$(escape_tf "$GCP_PROJECT_ID")" ;;
            oci) printf 'compartment_id     = "%s"\n' "$(escape_tf "$OCI_COMPARTMENT_ID")" ;;
        esac
    } > "$tf_dir/terraform.tfvars"

    chmod 600 "$tf_dir/terraform.tfvars"
    if [ -n "${SUDO_USER:-}" ]; then
        chown -R "$SUDO_USER" "$INSTALL_DIR"
    fi
    print_info "terraform.tfvars created."

    # Terraform init & apply (run as the original user to preserve CLI auth)
    print_step "Running Terraform..."
    local tf_run="terraform"
    if [ -n "${SUDO_USER:-}" ]; then
        tf_run="sudo -u $SUDO_USER terraform"
    fi
    (cd "$tf_dir" && $tf_run init)
    (cd "$tf_dir" && $tf_run apply -auto-approve)

    # Capture outputs
    VAULT_PUBLIC_IP=$(cd "$tf_dir" && $tf_run output -raw vault_public_ip 2>/dev/null) || true
    local vault_url
    vault_url=$(cd "$tf_dir" && $tf_run output -raw vault_url 2>/dev/null) || true

    print_info "Infrastructure provisioned."

    # Health polling — wait for cloud-init to finish and Vault to start
    if [ -n "$VAULT_PUBLIC_IP" ]; then
        print_step "Waiting for Vault to become reachable (up to 10 min)..."
        local elapsed=0
        local timeout=600
        while [ $elapsed -lt $timeout ]; do
            if bash -c "echo >/dev/tcp/${VAULT_PUBLIC_IP}/50051" 2>/dev/null; then
                print_info "Vault is reachable at ${VAULT_PUBLIC_IP}:50051!"

                # Download ca.pem from remote server
                if [ "$TLS_MODE" = "self-signed" ]; then
                    mkdir -p "$INSTALL_DIR/certs"
                    if [ -n "${SUDO_USER:-}" ]; then
                        chown -R "$SUDO_USER" "$INSTALL_DIR/certs"
                    fi
                    local scp_opts="-i $ssh_key_path -o StrictHostKeyChecking=no -o ConnectTimeout=15 -o BatchMode=yes"
                    local scp_prefix=""
                    if [ -n "${SUDO_USER:-}" ]; then
                        scp_prefix="sudo -u $SUDO_USER"
                    fi
                    # Retry SCP (SSH may not be ready immediately)
                    local downloaded=0
                    for attempt in 1 2 3; do
                        sleep 10
                        for ssh_user in ubuntu opc; do
                            if $scp_prefix scp $scp_opts \
                                "${ssh_user}@${VAULT_PUBLIC_IP}:/opt/rune/certs/ca.pem" \
                                "$INSTALL_DIR/certs/ca.pem" 2>/dev/null; then
                                downloaded=1; break 2
                            fi
                        done
                    done
                    if [ "$downloaded" -eq 1 ]; then
                        CSP_CA_CERT_LOCAL="$INSTALL_DIR/certs/ca.pem"
                        print_info "CA certificate downloaded to ${CSP_CA_CERT_LOCAL}"
                    else
                        print_warn "Could not download ca.pem via SSH. Retrieve manually:"
                        echo "  scp -i ${ssh_key_path} ubuntu@${VAULT_PUBLIC_IP}:/opt/rune/certs/ca.pem ${INSTALL_DIR}/certs/"
                    fi
                fi

                break
            fi
            sleep 10
            elapsed=$((elapsed + 10))
            printf "."
        done
        echo ""
        if [ $elapsed -ge $timeout ]; then
            print_error "Vault not reachable within ${timeout}s. Cloud-init may still be running."
            echo ""
            echo "  Debug via SSH:"
            echo "  ssh -i ${ssh_key_path} ubuntu@${VAULT_PUBLIC_IP} 'cloud-init status --wait && docker ps'"
            echo ""
            echo "  Terraform directory: ${tf_dir}"
            echo "  To destroy resources: cd ${tf_dir} && terraform destroy"
            exit 1
        fi
    fi
}

# ─── Summary ──────────────────────────────────────────────────────────────────

show_summary() {
    local endpoint
    if [ "$DEPLOY_TARGET" = "local" ]; then
        if [ "$TLS_MODE" = "none" ]; then
            endpoint="localhost:50051"
        else
            endpoint="localhost:50051 (TLS)"
        fi
    else
        local ip="${VAULT_PUBLIC_IP:-<public-ip>}"
        endpoint="${ip}:50051"
    fi

    print_header "Deployment Complete (DEV)"
    echo "  Vault Endpoint  : ${endpoint}"
    echo "  Docker Image    : ${DOCKER_IMAGE}:${DOCKER_TAG} (local build)"
    echo "  Team Secret     : (stored in ${INSTALL_DIR}/.env)"
    echo "  TLS Mode        : ${TLS_MODE}"
    if [ "$TLS_MODE" = "self-signed" ] && [ "$DEPLOY_TARGET" = "local" ]; then
        echo "  CA Certificate  : ${INSTALL_DIR}/certs/ca.pem"
    fi
    echo ""
    echo -e "${BOLD}Share with your team:${NC}"
    echo ""
    echo "  Team members will need the following credentials when installing the"
    echo "  Rune plugin/extension. Share them securely (e.g. encrypted channel):"
    echo ""
    if [ -n "${TLS_HOSTNAME:-}" ]; then
        echo "  Endpoint : ${TLS_HOSTNAME}:50051"
    elif [ "$DEPLOY_TARGET" != "local" ] && [ -n "${VAULT_PUBLIC_IP:-}" ]; then
        echo "  Endpoint : ${VAULT_PUBLIC_IP}:50051"
    else
        echo "  Endpoint : <public-ip>:50051"
    fi
    echo ""
    echo "  Issue per-user tokens with:"
    echo "    runevault token issue --user <name> --role agent --expires 90d"
    echo ""
    echo "  Each team member uses their individual token for authentication."
    echo "  Team Secret (above) is only needed for DEK derivation — keep it secure."
    if [ "$TLS_MODE" = "self-signed" ]; then
        echo ""
        echo "  Your vault uses a self-signed CA. Team members also need the CA"
        echo "  certificate file below. Share this file directly — they will be"
        echo "  prompted to provide its path during plugin/extension setup."
        echo ""
        if [ -n "${CSP_CA_CERT_LOCAL}" ]; then
            echo "  CA Cert  : ${CSP_CA_CERT_LOCAL}"
        elif [ "$DEPLOY_TARGET" = "local" ]; then
            echo "  CA Cert  : ${INSTALL_DIR}/certs/ca.pem"
        else
            echo "  CA Cert  : /opt/rune/certs/ca.pem (on the remote server)"
        fi
    fi
    if [ "$DEPLOY_TARGET" != "local" ]; then
        echo ""
        echo -e "${BOLD}Next steps:${NC}"
        echo "  1. Point your domain DNS to ${VAULT_PUBLIC_IP:-<public-ip>}"
        echo "  2. To use custom TLS certificates, replace files in /opt/rune/certs/ on the server"
        echo "     and restart: ssh -i ${INSTALL_DIR}/ssh_key ubuntu@${VAULT_PUBLIC_IP:-<public-ip>} 'cd /opt/rune && docker compose restart'"
        echo ""
        echo "  SSH access: ssh -i ${INSTALL_DIR}/ssh_key ubuntu@${VAULT_PUBLIC_IP:-<public-ip>}"
    fi
    echo ""
    echo "Install directory: ${INSTALL_DIR}"
    echo ""
}

# ─── main() ──────────────────────────────────────────────────────────────────

main() {
    print_header "Rune-Vault Interactive Setup (DEV)"
    echo "Local development installer — uses files from the working tree."
    echo "Repo: ${REPO_ROOT}"
    echo ""

    # 1. Deployment target
    choose_deploy_target

    # 2. Prerequisites
    if [ "$DEPLOY_TARGET" = "local" ]; then
        check_prerequisites_local
    else
        check_prerequisites_csp "$DEPLOY_TARGET"
    fi

    # 3. Install directory
    prompt_install_dir

    # 4. Common settings
    prompt_tls_mode
    generate_team_secret
    prompt_envector_config

    # 5. CSP-specific settings
    if [ "$DEPLOY_TARGET" != "local" ]; then
        prompt_csp_config
    fi

    # 6. Confirm
    show_confirmation

    # 7. Deploy
    if [ "$DEPLOY_TARGET" = "local" ]; then
        deploy_local
    else
        deploy_csp
    fi

    # 8. Summary
    show_summary
}

main "$@"
