# OCI Rune-Vault Deployment

This directory contains Terraform configuration for deploying Rune-Vault on Oracle Cloud Infrastructure (OCI).

## Prerequisites

1. **OCI Account**: Sign up at https://cloud.oracle.com
2. **OCI CLI**: Install from https://docs.oracle.com/en-us/iaas/Content/API/SDKDocs/cliinstall.htm
3. **Terraform**: Install from https://www.terraform.io/downloads
4. **SSH Key**: `~/.ssh/id_rsa.pub` (or update in main.tf)

## Quick Start

### 1. Configure OCI CLI

```bash
oci setup config
```

Follow prompts to configure:
- User OCID
- Tenancy OCID
- Region
- API key

### 2. Set Variables

Create `terraform.tfvars`:

```hcl
compartment_id = "ocid1.compartment.oc1..xxx"
team_name      = "myteam"
region         = "us-ashburn-1"  # Optional, defaults to us-ashburn-1
```

### 3. Deploy

```bash
# Initialize Terraform
terraform init

# Preview changes
terraform plan

# Deploy
terraform apply

# Output:
# vault_url = "https://vault-myteam.us-ashburn-1.oci.envector.io"
# vault_token = "evt_myteam_xxx" (sensitive)
# vault_public_ip = "xxx.xxx.xxx.xxx"
```

### 4. Configure DNS

Point your domain to the Vault instance:

```bash
# Get public IP
terraform output vault_public_ip

# Create DNS A record:
vault-myteam.us-ashburn-1.oci.envector.io → xxx.xxx.xxx.xxx
```

### 5. Obtain SSL Certificate

SSH into instance and run:

```bash
# SSH to instance
terraform output ssh_command | bash

# Get SSL certificate
sudo certbot --nginx -d vault-myteam.us-ashburn-1.oci.envector.io

# Verify
curl https://vault-myteam.us-ashburn-1.oci.envector.io/health
```

### 6. Share Credentials with Team

```bash
# Get credentials
terraform output vault_url
terraform output vault_token

# Share with team (use secure channel)
export RUNEVAULT_ENDPOINT=$(terraform output -raw vault_url)
export RUNEVAULT_TOKEN=$(terraform output -raw vault_token)
```

## Architecture

```
┌─────────────────────────────────────────┐
│         OCI (Your Account)              │
│  ┌───────────────────────────────────┐  │
│  │  VCN (10.0.0.0/16)                │  │
│  │  ┌─────────────────────────────┐  │  │
│  │  │  Subnet (10.0.1.0/24)       │  │  │
│  │  │  ┌───────────────────────┐  │  │  │
│  │  │  │ Compute Instance      │  │  │  │
│  │  │  │ - Ubuntu 22.04        │  │  │  │
│  │  │  │ - 1 OCPU, 4GB RAM     │  │  │  │
│  │  │  │                       │  │  │  │
│  │  │  │ Docker:               │  │  │  │
│  │  │  │ └─ rune-vault:50080   │  │  │  │
│  │  │  │                       │  │  │  │
│  │  │  │ Nginx (SSL):443       │  │  │  │
│  │  │  └───────────────────────┘  │  │  │
│  │  │         Public IP           │  │  │
│  │  └─────────────────────────────┘  │  │
│  └───────────────────────────────────┘  │
└─────────────────────────────────────────┘
                  ▲
                  │ HTTPS
                  │
           ┌──────┴──────┐
           │  Your Team  │
           └─────────────┘
```

## What Gets Deployed

1. **Networking**:
   - VCN (Virtual Cloud Network)
   - Public subnet
   - Internet Gateway
   - Security List (ports 443, 50080, 22)

2. **Compute**:
   - VM.Standard.E4.Flex (1 OCPU, 4GB RAM)
   - Ubuntu 22.04
   - Docker + Docker Compose
   - Nginx with SSL (Let's Encrypt)

3. **Rune-Vault**:
   - Docker container running on port 50080
   - Reverse proxy through Nginx (443)
   - **TLS encryption** for all API communications
   - Health checks enabled
   - Auto-restart on failure

## Security Features

### TLS/HTTPS

⚠️ **All Vault communications are TLS-encrypted by default.**

Our deployment includes:
- **Nginx reverse proxy** with TLS termination
- **Let's Encrypt** automatic certificate provisioning
- **HTTP → HTTPS redirect** (no plaintext traffic)
- **Strong cipher suites** (TLS 1.2+)

**Why this matters:**
- Vault tokens are sent in API requests
- Without TLS, tokens can be intercepted over the network
- FHE-encrypted data is also transmitted (defense in depth)

**Certificate renewal:**
```bash
# Automatic via certbot cron job
# Manual renewal if needed:
sudo certbot renew
sudo systemctl reload nginx
```

## Cost Estimation

**OCI Free Tier** (eligible):
- 2 VMs (VM.Standard.E2.1.Micro) - FREE
- 10TB outbound transfer/month - FREE
- 2 Block Volumes - FREE

**If not using Free Tier:**
- Compute: ~$30/month (VM.Standard.E4.Flex, 1 OCPU, 4GB)
- Network: ~$5/month
- **Total: ~$35/month**

## Security

### Keys Encryption

FHE keys are encrypted at rest:

```bash
# SSH to instance
ssh ubuntu@$(terraform output -raw vault_public_ip)

# Check keys encryption
cd /opt/rune/keys
ls -la

# Keys should be encrypted (*.enc files)
```

### Backup Keys

```bash
# Backup keys to S3-compatible storage
ssh ubuntu@$(terraform output -raw vault_public_ip)
sudo tar -czf /tmp/vault-keys-backup.tar.gz /opt/rune/keys
# Download and store securely
```

### Monitoring

Health check endpoint:

```bash
curl https://vault-myteam.oci.envector.io/health

# Expected:
# {"status": "healthy", "version": "0.1.0"}
```

## Troubleshooting

### Vault not responding

```bash
# SSH to instance
terraform output ssh_command | bash

# Check Docker container
docker ps
docker logs rune-vault

# Restart if needed
cd /opt/rune
docker-compose restart
```

### SSL certificate issues

```bash
# SSH to instance
ssh ubuntu@$(terraform output -raw vault_public_ip)

# Check nginx
sudo systemctl status nginx
sudo nginx -t

# Retry SSL
sudo certbot --nginx -d vault-myteam.oci.envector.io
```

### DNS not resolving

```bash
# Check DNS propagation
dig vault-myteam.oci.envector.io

# Verify A record points to correct IP
terraform output vault_public_ip
```

## Cleanup

```bash
# Destroy all resources
terraform destroy

# Confirm: yes
```

**Warning**: This will permanently delete:
- Compute instance
- VCN and networking
- FHE keys (backup first!)

## High Availability Setup

For production, deploy with standby Vault:

1. Deploy second Vault in different availability domain
2. Sync secret key securely
3. Configure health check failover
4. Use load balancer (OCI LB or external)

See [../docs/HA-SETUP.md](../../docs/HA-SETUP.md) for details.

## Next Steps

- Configure team members: `../../scripts/configure-agent.sh`
- Test integration: `../../examples/team-collaboration/`
- Monitor health: Setup CloudWatch/Prometheus
- Backup keys: Schedule regular backups

## Support

- **Issues**: https://github.com/CryptoLabInc/rune/issues
- **Docs**: https://docs.envector.io
- **Email**: support@cryptolab.co.kr
