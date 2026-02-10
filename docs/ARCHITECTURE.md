# Rune-Vault Architecture

## System Overview

Rune-Vault is the **infrastructure backbone** for team-shared FHE-encrypted organizational memory. It manages cryptographic keys, authenticates team members, and provides decryption services for encrypted search results.

### Core Responsibilities

1. **Key Management**: Generate, store, and protect FHE keys (secret key isolation)
2. **Decryption Service**: Decrypt search results from enVector Cloud
3. **Authentication**: Validate team member access via tokens
4. **Monitoring**: Track usage, performance, and security metrics

## High-Level Architecture

```
┌──────────────────────────────────────────────────┐
│                    Team Members                  │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐  │
│  │   Alice    │  │    Bob     │  │   Carol    │  │
│  │  (Claude)  │  │  (Gemini)  │  │  (Codex)   │  │
│  │            │  │            │  │            │  │
│  │    Rune    │  │    Rune    │  │    Rune    │  │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘  │
└────────┼───────────────┼───────────────┼─────────┘
         │               │               │
         └───────────────┴───────────────┘
                         │ MCP tool calls
                         ▼
            ┌────────────────────────────┐
            │   envector-mcp-server(s)   │  ← Scalable
            │   (Public Keys only)       │
            │                            │
            │  Tools:                    │
            │  - insert, search (direct) │
            │  - remember (Vault pipeline│
            │    search → decrypt → meta)│
            └──────┬──────────────┬──────┘
                   │              │
    search/insert  │              │ decrypt_scores()
                   │              │ (called by remember)
                   ▼              ▼
  ┌──────────────────────┐  ┌────────────────────────────┐
  │ enVector Cloud(SaaS) │  │      Rune-Vault MCP        │
  │  https://envector.io │  │  (Your Infrastructure)     │
  │                      │  │                            │
  │  - Encrypted vectors │  │  ┌──────────────────────┐  │
  │  - Encrypted         │  │  │  FHE Key Manager     │  │
  │    similarity search │  │  │                      │  │
  │  - Team isolation    │  │  │  - secret key (isolated)│  │
  │  - Scalable storage  │  │  │  - EncKey (public)   │  │
  └──────────────────────┘  │  └──────────────────────┘  │
                            │                            │
                            │  ┌──────────────────────┐  │
                            │  │  MCP Tools           │  │
                            │  │  - get_public_key()  │  │
                            │  │  - decrypt_scores()  │  │
                            │  └──────────────────────┘  │
                            │                            │
                            │  ┌──────────────────────┐  │
                            │  │  Auth & Monitoring   │  │
                            │  │  - Token validation  │  │
                            │  │  - Prometheus metrics│  │
                            │  └──────────────────────┘  │
                            └────────────────────────────┘
```

**Key**: Agents never contact Vault directly. The envector-mcp-server's
`remember` tool orchestrates the Vault decryption call as part of its
3-step pipeline. Secret key never leaves Vault.

## Component Details

### 1. Rune-Vault MCP Server

**Purpose**: Centralized key management and decryption service for a team

**Deployment Options**:
- **OCI** (Oracle Cloud Infrastructure)
- **AWS** (Elastic Compute Cloud)
- **GCP** (Google Compute Engine)
- **On-Premise** (Self-hosted)

**Runtime**:
- Python 3.8+ with FastMCP framework
- uvicorn ASGI server
- Prometheus metrics exporter
- System monitoring (psutil)

**Key Storage**:
```
/vault_keys/
├── EncKey.json      # Public encryption key (distributed to team members)
├── EvalKey.json     # Public evaluation key (for FHE operations)
├── MetadataKey.json # Public metadata key
└── SecKey.json      # Secret decryption key (NEVER leaves Vault)
```

**Security Properties**:
- Secret key stored encrypted at rest (filesystem encryption)
- Keys loaded into memory only during operations
- No secret key export API (architectural constraint)
- TLS for all network communications
- Token-based authentication per request

### 2. MCP Tools (API)

**`get_public_key()`**
- Returns: EncKey, EvalKey, MetadataKey (JSON bundle)
- Used by: Team members (one-time at startup)
- Auth: Required (validates token)
- Rate Limit: None (lightweight operation)

**`decrypt_scores()`**
- Input: Result ciphertext from encrypted similarity search (base64-serialized)
- Returns: Top-K similarity values with indices (decrypted from ciphertext)
- Used by: Team members (per search query)
- Auth: Required (validates token)
- Rate Limit: Yes (default 10 results per call, configurable)

### 3. Authentication System

**Token Format**: `evt_{team}_{random}`
- Example: `evt_yourteam_abc123xyz`
- Generated during Terraform deployment
- Shared with all team members (same token for whole team)

**Token Validation**:
```python
VALID_TOKENS = {
    "evt_yourteam_abc123xyz",  # Team token
    "evt_admin_master",        # Admin token (optional)
}

def validate_token(token: str) -> bool:
    return token in VALID_TOKENS
```

**Token Rotation**:
```bash
# Generate new token via Terraform
terraform apply -var="rotate_token=true"

# Distribute new token to team
# Old token invalidated immediately
```

### 4. Monitoring & Observability

**Prometheus Metrics** (exposed at `/metrics`):
```
# Decryption operations
vault_decryption_requests_total
vault_decryption_latency_seconds{quantile="0.5|0.95|0.99"}
vault_decryption_errors_total

# Authentication
vault_auth_attempts_total
vault_auth_failures_total

# System health
vault_uptime_seconds
vault_memory_usage_bytes
vault_cpu_usage_percent
```

**Health Check** (`/health`):
```json
{
  "status": "healthy",
  "vault_version": "0.2.0",
  "fhe_keys_loaded": true,
  "uptime_seconds": 3600
}
```

**Grafana Dashboards**:
- See [deployment/monitoring/grafana/](../deployment/monitoring/grafana/) for templates
- Pre-configured alerts for high error rates, latency spikes

## Data Flow

### Client Initialization (One-Time)

```
Team Member's Laptop
    │
    ├── 1. Install Rune from Claude Marketplace
    ├── 2. Configure Vault URL + Token
    │
    ▼
Rune Startup
    │
    ├── 3. Call get_public_key() → Vault MCP
    │
    ▼
Vault MCP
    │
    ├── 4. Validate token
    ├── 5. Read /vault_keys/EncKey.json, EvalKey.json, MetadataKey.json
    ├── 6. Return public keys bundle
    │
    ▼
Rune Client
    │
    └── 7. Store keys locally, use for encryption
```

### Recall Query via `remember` (Runtime)

```
User: "What decisions did we make about database?"
    │
    ▼
AI Agent (Claude/Gemini/Codex)
    │
    ├── 1. Call envector-mcp-server `remember` tool
    │
    ▼
envector-mcp-server (`remember` orchestration)
    │
    ├── 2. Embed query (auto-embedded if text, or accepts vector/JSON)
    ├── 3. Encrypted similarity scoring on enVector Cloud
    │      → Returns result ciphertext (base64)
    │
    ├── 4. Call Vault: decrypt_scores(token, ciphertext, top_k)
    │
    ▼
Vault MCP (secret key holder)
    │
    ├── 5. Validate token
    ├── 6. Decrypt result ciphertext with secret key → similarity values
    ├── 7. Select top-k (max 10, enforced by Vault policy)
    ├── 8. Return [{index: 42, score: 0.95}, ...]
    │
    ▼
envector-mcp-server (continued)
    │
    ├── 9. Retrieve metadata for top-k indices from enVector Cloud
    ├── 10. Return results to Agent
    │
    ▼
AI Agent → User: "In Q2 2024, team chose PostgreSQL for JSON support..."
```

**Key**: The Agent never contacts Vault directly. The `remember` tool
in envector-mcp-server orchestrates the entire 3-step pipeline.
Secret key never leaves Vault.

**`search` vs `remember`**: The `search` tool is for the operator's own
encrypted data where secret key is held locally by the MCP server runtime.
The `remember` tool accesses shared team memory where secret key is held
exclusively by Rune-Vault, preventing agent tampering attacks from
indiscriminately decrypting shared vectors.

## Security Model

### Threat Model

**Assumptions**:
- enVector Cloud is **untrusted** (sees only ciphertext)
- Network is **untrusted** (TLS required)
- Team members' laptops are **trusted** (Rune runs locally)
- Vault VM is **trusted** (admin controls infrastructure)

**Threats Mitigated**:
1. **Cloud Provider Breach**: enVector Cloud compromise → no plaintext leak (FHE)
2. **Network Eavesdropping**: MITM attacks → TLS encryption
3. **Unauthorized Access**: Non-team members → token authentication
4. **Key Theft**: secret key extraction → architectural isolation (no export API)

**Threats Not Mitigated** (out of scope):
- Vault VM compromise (admin responsibility: use secure cloud, enable disk encryption)
- Team member laptop compromise (user responsibility: secure devices)
- Token leakage (admin responsibility: rotate tokens, use secure distribution)

### Key Isolation Strategy

**Why Secret Key Never Leaves Vault**:
- **Principle**: Decryption capability = highest privilege
- **Constraint**: Only Vault MCP has secret key, no export API
- **Benefit**: Even if client compromised, attacker cannot decrypt historical data

**Key Distribution**:
```
Secret key:  Vault only (generated on deployment, never exported)
EncKey:  Distributed to all team members (safe to share, encryption-only)
EvalKey: Distributed to all team members (safe to share, FHE operations)
```

### Defense in Depth

**Layer 1: Network**
- TLS 1.3 for all Vault communications
- Firewall rules (allow only HTTPS 443)
- Optional: VPN for extra isolation

**Layer 2: Authentication**
- Token validation on every request
- Rate limiting (prevent abuse)
- Audit logging (track who accesses what)

**Layer 3: Cryptography**
- FHE encryption (data encrypted at source)
- Keys encrypted at rest (filesystem encryption)
- Secure key generation (crypto-safe randomness)

**Layer 4: Monitoring**
- Prometheus alerts (unusual access patterns)
- Grafana dashboards (real-time visibility)
- Audit logs (compliance reporting)

## Deployment Architecture

### Cloud Deployment (Terraform)

```
Terraform Configuration
    │
    ├── deployment/oci/main.tf    # Oracle Cloud
    ├── deployment/aws/main.tf    # Amazon Web Services
    └── deployment/gcp/main.tf    # Google Cloud Platform
        │
        ▼
Cloud Resources Created
    │
    ├── Compute Instance (VM)
    │   ├── OS: Ubuntu 22.04 LTS
    │   ├── Shape: 2 OCPU, 8GB RAM, 50GB disk
    │   └── Software:
    │       ├── Python 3.8+
    │       ├── FastMCP
    │       ├── pyenvector (FHE SDK)
    │       └── Prometheus exporter
    │
    ├── Networking
    │   ├── Public IP address
    │   ├── Security group (allow 443/HTTPS)
    │   └── DNS: vault-{team}.oci.envector.io
    │
    ├── Storage
    │   ├── /vault_keys/ (encrypted volume)
    │   └── Backup to cloud storage (optional)
    │
    └── Monitoring
        ├── Prometheus scraper
        └── Grafana dashboard
```

### High Availability (Optional)

```
Load Balancer (HTTPS)
    │
    ├── Vault Instance 1 (Primary)
    ├── Vault Instance 2 (Standby)
    └── Vault Instance N (Standby)
        │
        └── Shared Storage (NFS/EFS)
            └── /vault_keys/ (same keys across instances)
```

**Setup**:
```bash
cd deployment/oci
terraform apply -var="ha_enabled=true" \
                -var="instance_count=3"
```

**Failover**:
- Health checks every 10s
- Auto-failover <30s
- Shared keys (no key synchronization needed)

## Operational Considerations

### Backup & Recovery

**Critical Assets**:
- `/vault_keys/SecKey.json` - **MUST backup** (cannot regenerate)
- `/vault_keys/EncKey.json` - Regenerable from secret key
- Vault token - Rotatable via Terraform

**Backup Strategy**:
```bash
# Automated backup (run daily via cron)
./scripts/backup-keys.sh

# Output:
# vault_keys_backup_2026-02-04.tar.gz.enc
# (encrypted with team password)

# Store in:
# 1. Offline (USB drive in safe)
# 2. Cloud (different provider, encrypted)
# 3. Password manager (1Password secure notes)
```

**Recovery Procedure**:
```bash
# If Vault VM fails
cd deployment/oci
terraform apply -var="restore_from_backup=true" \
                -var="backup_path=/path/to/backup.tar.gz.enc"

# Vault restarts with same keys
# Team members continue without reconfiguration
```

### Scaling Strategies

**Vertical Scaling** (increase VM size):
```bash
terraform apply -var="instance_shape=VM.Standard.E4.Flex" \
                -var="instance_ocpu=4" \
                -var="instance_memory_gb=16"
```

**Horizontal Scaling** (add more instances):
```bash
terraform apply -var="ha_enabled=true" \
                -var="instance_count=3"
```

**When to Scale**:
- CPU >80% sustained → Add OCPU or scale out
- Latency P95 >200ms → Add instances
- Error rate >1% → Investigate (likely config issue, not scale)

### Monitoring & Alerts

**Critical Alerts** (PagerDuty/Slack):
- Vault down (health check fails)
- Error rate >5%
- Latency P95 >500ms
- Disk usage >80%

**Warning Alerts** (Email):
- CPU >70% for >10min
- Memory >75%
- Unusual access patterns (spike in queries)

**Dashboards**:
- Real-time: Grafana (`/grafana/vault-dashboard.json`)
- Historical: Prometheus (`/prometheus/queries.yml`)

## Troubleshooting

### Issue: High Latency

**Symptoms**: decrypt_scores() taking >200ms

**Diagnosis**:
```bash
# Check Vault CPU
curl https://vault-yourteam.oci.envector.io/metrics | grep cpu

# Check FHE key dimension
# (higher dim = more accurate but slower)
cat /vault_keys/SecKey.json | jq '.dim'
```

**Solutions**:
- CPU bottleneck → Scale up (add OCPU)
- Large Top-K → Reduce max results (10 → 5)
- High dimension → Acceptable (dim=2048 is standard)

### Issue: Authentication Failures

**Symptoms**: Clients can't connect, 401 Unauthorized

**Diagnosis**:
```bash
# Check token is correct
echo $VAULT_TOKEN

# Verify Vault sees requests
ssh admin@vault-yourteam.oci.envector.io
sudo journalctl -u vault | grep "401"
```

**Solutions**:
- Wrong token → Re-share correct token
- Token rotated → Distribute new token to all team members
- Firewall → Check security group allows 443 from team IPs

### Issue: Vault Crashed

**Symptoms**: Health check fails, 503 Service Unavailable

**Diagnosis**:
```bash
ssh admin@vault-yourteam.oci.envector.io
sudo systemctl status vault
sudo journalctl -u vault -n 100
```

**Solutions**:
- OOM killer → Increase VM memory
- Disk full → Rotate logs (`logrotate`)
- Crashed process → Restart (`systemctl restart vault`)
- Persistent crash → Redeploy with backup keys

## Next Steps

- Deploy your first Vault: [Quick Start](../README.md#quick-start)
- Team setup guide: [TEAM-SETUP.md](TEAM-SETUP.md)
- Load testing: [Load Testing Guide](../tests/load/README.md)
- Monitoring setup: [Monitoring Guide](../deployment/monitoring/README.md)
