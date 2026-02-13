# Rune-Admin

**Infrastructure & Team Management for Rune-Vault**

Deploy and manage Rune-Vault infrastructure for your team. This repository contains deployment automation, monitoring, and team onboarding tools for administrators.

## What is Rune-Admin?

Rune-Admin provides **infrastructure management** for Rune-Vault:

- **🚀 Deployment**: Automated Vault deployment to OCI, AWS, or GCP
- **🔑 Key Management**: FHE encryption key generation and secure storage
- **👥 Team Onboarding**: Distribute credentials securely to team members
- **📊 Monitoring**: Prometheus metrics, Grafana dashboards, health checks
- **⚡ Load Testing**: Validate Vault performance under load

## Prerequisites

### For Administrators

1. **Python 3.8+** with pip and virtualenv
2. **Terraform** for cloud infrastructure deployment
3. **enVector Cloud account** at [https://envector.io](https://envector.io)
   - Organization ID and API Key
4. **Cloud provider account** (OCI, AWS, or GCP)

### For Team Members

Team members install Rune from Claude Marketplace and configure it with:
- Vault URL (provided by admin)
- Vault Token (provided by admin)

## Quick Start

### 1. Install Dependencies

```bash
# Clone repository
git clone https://github.com/CryptoLabInc/rune-admin.git
cd rune-admin

# Run interactive installer
./install.sh

# Choose role: Administrator
```

### 2. Deploy Rune-Vault

```bash
# Initialize Terraform
cd deployment/oci  # or aws, gcp
terraform init

# Configure variables
cp terraform.tfvars.example terraform.tfvars
# Edit: team_name, region, envector credentials

# Deploy
terraform apply
```

**Output**:
```
vault_url = "https://vault-yourteam.oci.envector.io"
vault_token = "evt_yourteam_abc123xyz"
```

### 3. Verify Deployment

```bash
# Test Vault health
curl https://vault-yourteam.oci.envector.io/health

# Expected: {"status": "healthy", "vault_version": "0.2.0"}
```

### 4. Onboard Team Members

Share Vault credentials with each team member:

**What you share (via secure channel):**
- Vault URL: `https://vault-yourteam.oci.envector.io`
- Vault Token: `evt_yourteam_xxx`

**What team members do:**
1. Install Rune from Claude Marketplace
2. Configure with Vault URL and token
3. Start using organizational memory

**Security best practices:**
- Use encrypted channels (1Password, Signal, etc.)
- Never share tokens in plain Slack/email
- Rotate tokens periodically

### 5. Monitor Vault

```bash
# View metrics
curl https://vault-yourteam.oci.envector.io/metrics

# Set up Grafana dashboard
cd deployment/monitoring
./setup-grafana.sh
```

## Architecture

```
┌────────────────────────────────────────────┐
│                  Team Members              │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │  Alice   │  │   Bob    │  │  Carol   │  │
│  │ (Claude) │  │ (Gemini) │  │ (Codex)  │  │
│  │  Agent   │  │  Agent   │  │  Agent   │  │
│  └────┬─────┘  └────┬─────┘  └────┬─────┘  │
└───────┼─────────────┼─────────────┼────────┘
        │             │             │
        └─────────────┴─────────────┘
                      │ MCP tool calls
                      ▼
          ┌─────────────────────────┐
          │  envector-mcp-server(s) │  ← Scalable Workers
          │  (Public Keys only)     │
          │                         │
          │  insert / search        │──→  enVector Cloud
          │  remember (3-step):     │     (Encrypted Storage)
          │    1. search            │──→  enVector Cloud
          │    2. decrypt           │──→  Rune-Vault MCP
          │    3. metadata          │──→  enVector Cloud
          └─────────────────────────┘
                                          │
                      ┌───────────────────┘
                      ▼
          ┌───────────────────────┐
          │    Rune-Vault MCP     │
          │ (Your Infrastructure) │
          │                       │
          │  - secret key (isolated)│
          │  - get_public_key()   │
          │  - decrypt_scores()   │
          │  - Auth & Monitoring  │
          └───────────────────────┘
```

**Key Points:**
- **ONE Vault per team** (centralized key management)
- Agents call envector-mcp-server tools; they never contact Vault directly
- **`search`**: Operator's own data; secret key held locally by MCP server runtime
- **`remember`**: Shared team memory; secret key held exclusively by Vault. Orchestrates: encrypted similarity scoring → Vault decrypts result ciphertext → retrieve metadata for top-k indices. This isolation prevents agent tampering attacks.
- Vault holds secret key (never exposed); MCP servers only have EncKey/EvalKey

## Repository Structure

```
rune-admin/
├── deployment/
│   ├── oci/           # Oracle Cloud deployment
│   ├── aws/           # AWS deployment
│   ├── gcp/           # GCP deployment
│   └── monitoring/    # Grafana + Prometheus
├── mcp/
│   └── vault/         # Rune-Vault MCP server
│       ├── run_vault.sh        # Local dev script
│       ├── verify_crypto_flow.py  # Crypto validation
│       └── vault_keys/         # Generated FHE keys
├── scripts/
│   ├── deploy-vault.sh        # Automated deployment
│   ├── configure-agent.sh     # Agent setup helper
│   └── vault-dev.sh           # Local Vault for testing
├── tests/
│   ├── unit/          # Unit tests
│   ├── integration/   # Integration tests
│   └── load/          # Load testing scripts
├── docs/
│   ├── ARCHITECTURE.md        # System architecture
│   └── TEAM-SETUP.md          # Team collaboration guide
└── install.sh         # Interactive installer
```

## Features

### ✅ Deployment Automation

```bash
# One command deployment
cd deployment/oci
terraform apply

# Auto-provisions:
# - VM instance
# - Security groups
# - SSL certificates
# - FHE key generation
# - Monitoring setup
```

### ✅ Key Management

```bash
# FHE keys auto-generated on deployment
/vault_keys/
├── EncKey.json      # Public (distributed to team members)
├── EvalKey.json     # Public (for FHE operations)
├── MetadataKey.json # Secret (NEVER leaves Vault)
└── SecKey.json      # Secret (NEVER leaves Vault)
```

### ✅ Monitoring

- Prometheus metrics (`/metrics` endpoint)
- Grafana dashboards (deployment/monitoring/)
- Health checks (`/health` endpoint)
- Audit logging

### ✅ Load Testing

```bash
cd tests/load
./run_load_test.sh

# Simulates:
# - 100 concurrent users
# - 1000 queries/minute
# - Reports P95 latency
```

## Admin Workflows

### Deploy New Vault

```bash
# 1. Configure Terraform
cd deployment/oci
cp terraform.tfvars.example terraform.tfvars
# Edit variables

# 2. Deploy
terraform apply

# 3. Save credentials (from Terraform output)
# vault_url, vault_token
```

### Onboard New Team Member

```bash
# 1. Share same Vault URL and token
# 2. Team member installs Rune and configures
# 3. No Vault changes needed - same keys work for everyone
```

### Monitor Vault

```bash
# Check metrics
curl https://vault-yourteam.oci.envector.io/metrics

# View Grafana dashboard
# http://grafana-yourteam.oci.envector.io

# Check logs
ssh admin@vault-yourteam.oci.envector.io
sudo journalctl -u vault -f
```

### Rotate Token

```bash
cd deployment/oci
terraform apply -var="rotate_token=true"

# Output: new_vault_token = "evt_yourteam_xyz789"

# Distribute new token to all team members
```

### Scale Vault

```bash
# Increase instance size
terraform apply -var="instance_shape=VM.Standard.E4.Flex" \
                -var="instance_memory_gb=32"

# Or add multiple instances + load balancer
terraform apply -var="ha_enabled=true"
```

## Security

### Token Management

**Security best practices:**
```bash
# ✅ Good: Environment variables
export RUNEVAULT_TOKEN="evt_xxx"

# ✅ Good: Encrypted config files
# ✅ Good: Team setup packages (secure distribution)

# ❌ Bad: Hardcoded in code
# ❌ Bad: Committed to git
# ❌ Bad: Shared in Slack/email plaintext
```

### TLS Requirement

⚠️ **CRITICAL**: Vault communications MUST use TLS (HTTPS)

**Why**: Vault tokens transmitted over network
- **Tokens** grant decryption access
- **Without TLS**: Tokens exposed to MITM attacks
- **With TLS**: Encrypted transport layer

**Setup**: Terraform automatically configures SSL certificates (Let's Encrypt)

### Key Isolation

- **Secret key**: Never leaves Vault VM (architectural constraint)
- **EncKey/EvalKey**: Safe to distribute (public keys)
- **Vault Token**: Rotate every 90 days

## Deployment Targets

### OCI (Oracle Cloud)
- **Setup**: [deployment/oci/README.md](deployment/oci/README.md)

### AWS (Amazon Web Services)
- **Setup**: [deployment/aws/README.md](deployment/aws/README.md)

### GCP (Google Cloud Platform)
- **Setup**: [deployment/gcp/README.md](deployment/gcp/README.md)

## Development

### Local Vault (Testing)

```bash
# Start local Vault for development
./scripts/vault-dev.sh

# Output:
# Vault URL: http://localhost:50080
# Token: demo_token_123 (INSECURE!)
```

### Run Tests

```bash
# Unit tests
cd tests
pytest unit/ -v

# Integration tests
pytest integration/ -v

# Load tests
cd load
./run_load_test.sh
```

## Troubleshooting

### Issue: Team member can't connect

```bash
# Check Vault is reachable
curl https://vault-yourteam.oci.envector.io/health

# Check firewall rules
cd deployment/oci
terraform state show oci_core_security_list.vault

# Verify token
# (Have team member re-enter carefully)
```

### Issue: Slow decryption

```bash
# Check Vault CPU usage
# Increase instance resources if >80%

# Check metrics
curl https://vault-yourteam.oci.envector.io/metrics | grep latency
```

### Issue: Vault crashed

```bash
# Check logs
ssh admin@vault-yourteam.oci.envector.io
sudo journalctl -u vault -n 100

# Restart
sudo systemctl restart vault

# If persistent, redeploy
cd deployment/oci
terraform destroy
terraform apply
```

## Documentation

- **Architecture**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- **Team Setup**: [docs/TEAM-SETUP.md](docs/TEAM-SETUP.md)
- **OCI Deployment**: [deployment/oci/README.md](deployment/oci/README.md)
- **AWS Deployment**: [deployment/aws/README.md](deployment/aws/README.md)
- **GCP Deployment**: [deployment/gcp/README.md](deployment/gcp/README.md)
- **Load Testing**: [tests/load/README.md](tests/load/README.md)

## Support

- **Issues**: https://github.com/CryptoLabInc/rune-admin/issues
- **Discussions**: https://github.com/CryptoLabInc/rune-admin/discussions
- **Email**: support@envector.io

## Related Repositories

- **[enVector](https://github.com/CryptoLabInc/envector)**: FHE-encrypted vector database
- **[pyenvector](https://pypi.org/project/pyenvector/)**: Python SDK for enVector Cloud

## License

MIT License - see [LICENSE](LICENSE) for details.

---

**Remember**: This repo is for **administrators** managing Rune-Vault infrastructure.
