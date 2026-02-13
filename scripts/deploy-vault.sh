#!/bin/bash
set -e

# Deploy team Vault to cloud provider

VERSION="0.1.0"
PROVIDER=""
TEAM_NAME=""
REGION=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

usage() {
    cat << EOF
Rune-Vault Deployment v${VERSION}

Usage: $0 --provider <provider> --team-name <name> [options]

Required:
  --provider <type>     Cloud provider: oci, aws, gcp, on-premise
  --team-name <name>    Your team name (used for endpoint)

Optional:
  --region <region>     Cloud region (default: provider-specific)

Examples:
  $0 --provider oci --team-name myteam --region us-ashburn-1
  $0 --provider aws --team-name myteam --region us-east-1
  $0 --provider on-premise --team-name myteam

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

deploy_oci() {
    log_info "Deploying Vault to Oracle Cloud Infrastructure..."
    
    REGION="${REGION:-us-ashburn-1}"
    RUNEVAULT_ENDPOINT="https://vault-${TEAM_NAME}.oci.envector.io"
    
    log_info "Using deployment configuration from: deployment/oci/"
    
    # Check if OCI CLI is installed
    if ! command -v oci &> /dev/null; then
        log_error "OCI CLI not found. Install: https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm"
        exit 1
    fi
    
    # Deploy using Terraform
    cd deployment/oci
    terraform init
    terraform apply \
        -var="team_name=${TEAM_NAME}" \
        -var="region=${REGION}" \
        -auto-approve
    
    # Get outputs
    RUNEVAULT_ENDPOINT=$(terraform output -raw vault_url)
    RUNEVAULT_TOKEN=$(terraform output -raw vault_token)
    
    log_info "✓ Vault deployed successfully!"
    echo ""
    echo "${GREEN}Vault Endpoint:${NC} $RUNEVAULT_ENDPOINT"
    echo "${GREEN}Team Token:${NC} $RUNEVAULT_TOKEN"
    echo ""
    echo "${YELLOW}Share these credentials with your team:${NC}"
    echo "export RUNEVAULT_ENDPOINT=\"$RUNEVAULT_ENDPOINT\""
    echo "export RUNEVAULT_TOKEN=\"$RUNEVAULT_TOKEN\""
}

deploy_aws() {
    log_info "Deploying Vault to Amazon Web Services..."
    
    REGION="${REGION:-us-east-1}"
    RUNEVAULT_ENDPOINT="https://vault-${TEAM_NAME}.aws.envector.io"
    
    log_info "Using deployment configuration from: deployment/aws/"
    
    # Check if AWS CLI is installed
    if ! command -v aws &> /dev/null; then
        log_error "AWS CLI not found. Install: https://aws.amazon.com/cli/"
        exit 1
    fi
    
    # Deploy using Terraform
    cd deployment/aws
    terraform init
    terraform apply \
        -var="team_name=${TEAM_NAME}" \
        -var="region=${REGION}" \
        -auto-approve
    
    # Get outputs
    RUNEVAULT_ENDPOINT=$(terraform output -raw vault_url)
    RUNEVAULT_TOKEN=$(terraform output -raw vault_token)
    
    log_info "✓ Vault deployed successfully!"
    echo ""
    echo "${GREEN}Vault Endpoint:${NC} $RUNEVAULT_ENDPOINT"
    echo "${GREEN}Team Token:${NC} $RUNEVAULT_TOKEN"
    echo ""
    echo "${YELLOW}Share these credentials with your team:${NC}"
    echo "export RUNEVAULT_ENDPOINT=\"$RUNEVAULT_ENDPOINT\""
    echo "export RUNEVAULT_TOKEN=\"$RUNEVAULT_TOKEN\""
}

deploy_gcp() {
    log_info "Deploying Vault to Google Cloud Platform..."
    
    REGION="${REGION:-us-central1}"
    RUNEVAULT_ENDPOINT="https://vault-${TEAM_NAME}.gcp.envector.io"
    
    log_info "Using deployment configuration from: deployment/gcp/"
    
    # Check if gcloud is installed
    if ! command -v gcloud &> /dev/null; then
        log_error "gcloud CLI not found. Install: https://cloud.google.com/sdk/docs/install"
        exit 1
    fi
    
    # Deploy using Terraform
    cd deployment/gcp
    terraform init
    terraform apply \
        -var="team_name=${TEAM_NAME}" \
        -var="region=${REGION}" \
        -auto-approve
    
    # Get outputs
    RUNEVAULT_ENDPOINT=$(terraform output -raw vault_url)
    RUNEVAULT_TOKEN=$(terraform output -raw vault_token)
    
    log_info "✓ Vault deployed successfully!"
    echo ""
    echo "${GREEN}Vault Endpoint:${NC} $RUNEVAULT_ENDPOINT"
    echo "${GREEN}Team Token:${NC} $RUNEVAULT_TOKEN"
    echo ""
    echo "${YELLOW}Share these credentials with your team:${NC}"
    echo "export RUNEVAULT_ENDPOINT=\"$RUNEVAULT_ENDPOINT\""
    echo "export RUNEVAULT_TOKEN=\"$RUNEVAULT_TOKEN\""
}

deploy_on_premise() {
    log_info "Deploying Vault on-premise..."

    RUNEVAULT_ENDPOINT="https://vault.${TEAM_NAME}.internal"

    log_info "Using deployment configuration from: mcp/vault/"

    # Check if Docker is installed
    if ! command -v docker &> /dev/null; then
        log_error "Docker not found. Install: https://docs.docker.com/get-docker/"
        exit 1
    fi

    # Generate token
    RUNEVAULT_TOKEN="evt_${TEAM_NAME}_$(openssl rand -hex 16)"

    # Write token to .env
    cd mcp/vault
    echo "VAULT_TOKENS=$RUNEVAULT_TOKEN" > .env.production

    # Deploy using Docker Compose
    docker compose --env-file .env.production up -d vault-mcp

    log_info "✓ Vault deployed successfully!"
    echo ""
    echo -e "${GREEN}Vault Endpoint:${NC} $RUNEVAULT_ENDPOINT"
    echo -e "${GREEN}Team Token:${NC} $RUNEVAULT_TOKEN"
    echo ""
    echo -e "${YELLOW}Configure DNS to point vault.${TEAM_NAME}.internal to this server${NC}"
    echo ""
    echo -e "${YELLOW}Share these credentials with your team:${NC}"
    echo "export RUNEVAULT_ENDPOINT=\"$RUNEVAULT_ENDPOINT\""
    echo "export RUNEVAULT_TOKEN=\"$RUNEVAULT_TOKEN\""
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --provider)
            PROVIDER="$2"
            shift 2
            ;;
        --team-name)
            TEAM_NAME="$2"
            shift 2
            ;;
        --region)
            REGION="$2"
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
if [ -z "$PROVIDER" ]; then
    log_error "Missing required argument: --provider"
    usage
fi

if [ -z "$TEAM_NAME" ]; then
    log_error "Missing required argument: --team-name"
    usage
fi

# Main deployment
log_info "Rune-Vault Deployment v${VERSION}"
log_info "Provider: $PROVIDER"
log_info "Team: $TEAM_NAME"

case "$PROVIDER" in
    oci)
        deploy_oci
        ;;
    aws)
        deploy_aws
        ;;
    gcp)
        deploy_gcp
        ;;
    on-premise)
        deploy_on_premise
        ;;
    *)
        log_error "Unknown provider: $PROVIDER"
        usage
        ;;
esac

log_info "Deployment complete!"
