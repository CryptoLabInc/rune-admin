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

### Platform

- **macOS** or **Linux** (Windows is not supported — pyenvector requires Unix)

### For Administrators

1. **enVector Cloud account** at [https://envector.io](https://envector.io) — Organization ID and API Key
2. **Cloud provider account** (OCI, AWS, or GCP) — only needed for cloud deployment

The [installer](#quick-start) will automatically check and install required tools (Docker, Terraform, etc.).

### For Team Members

Team members install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace and configure it with:
- Vault Endpoint (provided by admin)
- Vault Token (provided by admin)

## Quick Start

### 1. Deploy Rune-Vault

```bash
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-admin/latest/install.sh -o install.sh && sudo bash install.sh
```

The installer will interactively guide you through:
- Cloud provider selection (AWS / GCP / OCI)
- enVector Cloud credentials
- TLS certificate generation (step-ca)
- Terraform-based VM provisioning

**Output**:
```
vault_endpoint = "vault-yourteam.oci.envector.io:50051"
vault_token = "evt_yourteam_abc123xyz"
ca.pem downloaded for TLS verification
```

### 2. Verify Deployment

```bash
curl --cacert ca.pem https://<your-vault-host>/health

# Expected: {"status": "healthy", "vault_version": "0.1.0"}
```

### 3. Onboard Team Members

Share Vault credentials with each team member:

**What you share (via secure channel):**
- Vault Endpoint: `vault-yourteam.oci.envector.io:50051`
- Vault Token: `evt_yourteam_xxx`

**What team members do:**
1. Install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace
2. Configure with Vault Endpoint and token
3. Start using organizational memory

**Security best practices:**
- Use encrypted channels (1Password, Signal, etc.)
- Never share tokens in plain Slack/email
- Rotate tokens periodically

### 4. Monitor Vault

```bash
# View metrics
curl https://vault-yourteam.oci.envector.io/metrics

# Grafana dashboard template
# See deployment/monitoring/grafana-dashboard.json
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
          │    2. decrypt (gRPC)    │──→  Rune-Vault
          │    3. metadata          │──→  enVector Cloud
          └─────────────────────────┘
                                          │
                      ┌───────────────────┘
                      ▼
          ┌───────────────────────┐
          │      Rune-Vault       │
          │ (Your Infrastructure) │
          │                       │
          │  - secret key (isolated)│
          │  - gRPC :50051        │
          │    GetPublicKey()     │
          │    DecryptScores()    │
          │    DecryptMetadata()  │
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
├── vault/             # Rune-Vault gRPC server
│   ├── vault_core.py          # Core business logic
│   ├── vault_grpc_server.py   # gRPC entry point
│   ├── monitoring.py          # Health & metrics
│   ├── Makefile               # Build & run commands
│   └── vault_keys/            # Generated FHE keys
├── scripts/
│   ├── vault-dev.sh           # Local Vault for testing
│   └── load-test.sh           # Load testing runner
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
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-admin/latest/install.sh -o install.sh && sudo bash install.sh

# Auto-provisions:
# - TLS certificates (step-ca)
# - VM instance (Terraform)
# - Security groups
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
./scripts/load-test.sh

# Simulates:
# - Configurable concurrent users
# - Key operations, decrypt, health check scenarios
# - Reports P95 latency via Locust
```

## Admin Workflows

### Deploy New Vault

```bash
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-admin/latest/install.sh -o install.sh && sudo bash install.sh

# The installer will guide you through:
# 1. Deployment target (Local / AWS / GCP / OCI)
# 2. TLS certificate generation
# 3. Terraform provisioning (for cloud targets)
# 4. Credential output (vault_endpoint, vault_token)
```

### Onboard New Team Member

```bash
# 1. Share same Vault Endpoint and token
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
- **Setup**: See `deployment/aws/main.tf`

### GCP (Google Cloud Platform)
- **Setup**: See `deployment/gcp/main.tf`

## Development

### Local Vault (Testing)

```bash
# Start local Vault for development
./scripts/vault-dev.sh

# Output:
# Vault gRPC:   localhost:50051
# Vault Health:  http://localhost:9090/health
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
../../scripts/load-test.sh
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
- **AWS Deployment**: `deployment/aws/main.tf`
- **GCP Deployment**: `deployment/gcp/main.tf`
- **Load Testing**: `scripts/load-test.sh`

## Support

- **Issues**: https://github.com/CryptoLabInc/rune-admin/issues
- **Discussions**: https://github.com/CryptoLabInc/rune-admin/discussions
- **Email**: zotanika@cryptolab.co.kr

## Related Repositories

- **[Rune](https://github.com/CryptoLabInc/rune)**: Claude plugin for organizational memory (what team members install)
- **[enVector](https://github.com/CryptoLabInc/envector)**: FHE-encrypted vector database
- **[pyenvector](https://pypi.org/project/pyenvector/)**: Python SDK for enVector Cloud

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.

---

**Remember**: This repo is for **administrators** managing Rune-Vault infrastructure.
