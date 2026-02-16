# Team Setup Guide for Administrators

## Overview

This guide shows administrators how to deploy and manage Rune-Vault infrastructure for team collaboration with organizational memory.

## Prerequisites

### For Administrators

1. **Cloud Account**: OCI, AWS, or GCP account with billing enabled
2. **Terraform**: Version 1.0+ installed
3. **enVector Cloud**: Sign up at [https://envector.io](https://envector.io)
   - Obtain Organization ID and API Key
4. **Security**: Secure channel for distributing credentials (1Password, Signal, etc.)

### For Team Members

Team members will need:
- [Rune](https://github.com/CryptoLabInc/rune) installed from Claude Marketplace (or their AI agent's marketplace)
- Vault Endpoint and token (provided by admin)

## Architecture

```
┌─────────────────────────────────────────────────────┐
│   enVector Cloud (https://envector.io)              │
│   Stores encrypted vectors (ciphertext only)        │
└─────────────────────────────────────────────────────┘
          ▲               ▲               ▲
          │ encrypted     │ encrypted     │ encrypted
┌─────────┴────┐  ┌───────┴──────┐  ┌─────┴────────┐
│   Alice      │  │     Bob      │  │    Carol     │
│   (Claude)   │  │   (Gemini)   │  │   (Codex)    │
│              │  │              │  │              │
│     Rune      │  │     Rune      │  │     Rune      │
│   (local)    │  │   (local)    │  │   (local)    │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       │ TLS             │ TLS             │ TLS
       └─────────────────┴─────────────────┘
                         │
                         ▼
              ┌──────────────────────┐
              │      Rune-Vault      │
              │  (Team Infrastructure)│
              │  - Holds secret key  │
              │  - Decrypts results  │
              │  - Distributes EncKey│
              └──────────────────────┘
```

**Key Points:**
- **ONE Vault per team** (not per developer)
- All team members connect to same Vault
- Same encryption keys = automatic context sharing
- Secret key never leaves Vault VM

## Step-by-Step Deployment

### Step 1: Deploy Rune-Vault

**Option A: OCI (Oracle Cloud) - Recommended**

```bash
cd deployment/oci

# Initialize Terraform
terraform init

# Review and customize variables
cp terraform.tfvars.example terraform.tfvars
# Edit: team_name, region, envector_org_id, envector_api_key

# Deploy
terraform apply

# Output:
# vault_endpoint = "vault-yourteam.oci.envector.io:50051"
# vault_token = "evt_yourteam_abc123xyz"
```

**Option B: AWS**

```bash
cd deployment/aws

terraform init
cp terraform.tfvars.example terraform.tfvars
# Edit variables

terraform apply
```

**Option C: GCP**

```bash
cd deployment/gcp

terraform init
cp terraform.tfvars.example terraform.tfvars
# Edit variables

terraform apply
```

**Option D: Local Development (Testing Only)**

```bash
./scripts/vault-dev.sh

# Output:
# Vault MCP:  http://localhost:50080
# Vault gRPC: localhost:50051
# Token: demo_token_123 (INSECURE - development only!)
```

### Step 2: Verify Deployment

```bash
# Test Vault health
curl https://vault-yourteam.oci.envector.io/health

# Expected response:
# {"status": "healthy", "vault_version": "0.1.0"}

# Check Prometheus metrics (optional)
curl https://vault-yourteam.oci.envector.io/metrics
```

### Step 3: Securely Distribute Credentials

**What to share with team members:**

```
Vault Endpoint: vault-yourteam.oci.envector.io:50051
Vault Token: evt_yourteam_abc123xyz
```

**How to share (choose one):**
- **1Password** or **Bitwarden**: Create shared vault item
- **Signal**: Encrypted messaging with disappearing messages
- **Encrypted email**: PGP-encrypted email
- **In-person**: Write on paper, shred after use

**Security checklist:**
- ✅ Use encrypted channel
- ✅ Never commit to Git
- ✅ Never send via plain Slack/Discord
- ✅ Document who has access
- ✅ Plan token rotation schedule

### Step 4: Team Member Onboarding

**Instructions for team members:**

1. Install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace (or your AI agent's marketplace)
2. Open plugin settings
3. Configure:
   - Vault Endpoint: `<received from admin>`
   - Vault Token: `<received from admin>`
4. Restart AI agent
5. Test: Ask agent "What organizational context do we have?"

**Verification:**
Each team member should see the same organizational memory instantly.

## Management Tasks

### Adding New Team Members

```bash
# 1. Share same Vault Endpoint and token
# 2. Team member installs plugin and configures
# 3. No Vault changes needed - same keys work for everyone
```

### Monitoring Vault Health

```bash
# Check uptime and metrics
curl https://vault-yourteam.oci.envector.io/metrics

# Key metrics:
# - vault_decryption_requests_total
# - vault_decryption_latency_seconds
# - vault_error_rate
```

Set up Grafana dashboard (see `deployment/monitoring/grafana-dashboard.json`)

### Token Rotation

```bash
# Generate new token
cd deployment/oci
terraform apply -var="rotate_token=true"

# Output: new_vault_token = "evt_yourteam_xyz789new"

# Distribute new token to all team members
# They update plugin settings
```

### Scaling (High Traffic)

```bash
# Increase Vault instance size
cd deployment/oci
terraform apply -var="instance_shape=VM.Standard.E4.Flex" \
                -var="instance_memory_gb=32"

# Or add load balancer for multiple Vault instances
```

### Backup and Recovery

```bash
# Backup FHE keys (CRITICAL - store securely!)
cd deployment/oci
terraform output vault_keys_backup

# Download encrypted keys
# Store in:
# - Offline storage (USB drive in safe)
# - Encrypted cloud backup (different provider)
# - Team password manager secure notes

# Recovery:
# If Vault VM fails, redeploy with backup keys
terraform apply -var="restore_from_backup=true" \
                -var="backup_keys_path=/path/to/keys"
```

### Troubleshooting

**Issue: Team member can't connect to Vault**

```bash
# Check Vault is reachable
curl https://vault-yourteam.oci.envector.io/health

# Check firewall rules
cd deployment/oci
terraform state show oci_core_security_list.vault

# Verify token is correct
# (Have team member re-enter token carefully)
```

**Issue: Slow decryption**

```bash
# Check Vault CPU usage
# Increase instance resources if >80% CPU

# Check metrics
curl https://vault-yourteam.oci.envector.io/metrics | grep latency
```

**Issue: Vault crashed**

```bash
# Check logs
ssh admin@vault-yourteam.oci.envector.io
sudo journalctl -u vault -n 100

# Restart Vault service
sudo systemctl restart vault

# If persistent, redeploy
cd deployment/oci
terraform destroy
terraform apply
```

## Advanced Configuration

### Multiple Teams/Projects

Deploy separate Vaults for each project:

```bash
# Project 1: Internal Tools
cd deployment/oci
terraform workspace new internal-tools
terraform apply -var="team_name=internal-tools"

# Project 2: Customer Project
terraform workspace new customer-alpha
terraform apply -var="team_name=customer-alpha"

# Team members switch by changing Vault Endpoint in plugin settings
```

### Custom Domain

```bash
# Instead of vault-yourteam.oci.envector.io
# Use vault.yourcompany.com

# Add CNAME record:
vault.yourcompany.com → vault-yourteam.oci.envector.io

# Update SSL certificate (see deployment/oci/dns/README.md)
```

### VPN/Private Network

```bash
# Deploy Vault in private subnet
cd deployment/oci
terraform apply -var="public_access=false" \
                -var="vpn_cidr=10.0.0.0/16"

# Team members connect via VPN
# (More secure for sensitive data)
```

## Security Best Practices

1. **Key Management**
   - Backup FHE keys to offline storage immediately after deployment
   - Never commit keys to Git
   - Rotate tokens every 90 days

2. **Access Control**
   - Document who has Vault token access
   - Revoke access for departing team members (rotate token)
   - Use separate Vaults for different security levels

3. **Network Security**
   - Always use TLS (HTTPS)
   - Consider VPN for high-security projects
   - Monitor access logs regularly

4. **Monitoring**
   - Set up alerts for high error rates
   - Monitor unusual access patterns
   - Regular health checks (automated)

## Next Steps

- Set up Grafana monitoring: `deployment/monitoring/grafana-dashboard.json`
- Load testing: `scripts/load-test.sh`
- Review architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Join community: https://github.com/CryptoLabInc/rune-admin/discussions
