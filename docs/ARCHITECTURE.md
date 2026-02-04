# Rune-Vault Architecture

## System Overview

Rune-Vault is the **infrastructure backbone** for team-shared FHE-encrypted organizational memory. It manages cryptographic keys, authenticates team members, and provides decryption services for encrypted search results.

### Core Responsibilities

1. **Key Management**: Generate, store, and protect FHE keys (SecKey isolation)
2. **Decryption Service**: Decrypt search results from enVector Cloud
3. **Authentication**: Validate team member access via tokens
4. **Monitoring**: Track usage, performance, and security metrics

## High-Level Architecture

```
┌──────────────────────────────────────────────────────────┐
│                    Team Members                          │
│  ┌────────────┐  ┌────────────┐  ┌────────────┐        │
│  │   Alice    │  │    Bob     │  │   Carol    │        │
│  │  (Claude)  │  │  (Gemini)  │  │  (Codex)   │        │
│  │            │  │            │  │            │        │
│  │    Rune    │  │    Rune    │  │    Rune    │        │
│  └─────┬──────┘  └─────┬──────┘  └─────┬──────┘        │
└────────┼───────────────┼───────────────┼────────────────┘
         │ TLS           │ TLS           │ TLS
         │               │               │
         └───────────────┴───────────────┘
                         │
                         ▼
            ┌────────────────────────────┐
            │      Rune-Vault MCP        │
            │  (Your Infrastructure)     │
            │                            │
            │  ┌──────────────────────┐  │
            │  │  FHE Key Manager     │  │
            │  │  - SecKey (isolated) │  │
            │  │  - EncKey (public)   │  │
            │  │  - EvalKey (public)  │  │
            │  └──────────────────────┘  │
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
                         │
                         │ (EncKey distribution)
                         ▼
            ┌────────────────────────────┐
            │    enVector Cloud (SaaS)   │
            │  https://envector.io       │
            │                            │
            │  - FHE-encrypted vectors   │
            │  - Semantic search (FHE)   │
            │  - Team data isolation     │
            │  - Scalable storage        │
            └────────────────────────────┘
```

## Component Details

### 1. Rune-Vault MCP Server

**Purpose**: Centralized key management and decryption service for a team

**Deployment Options**:
- **OCI** (Oracle Cloud Infrastructure) - Recommended, $30/mo
- **AWS** (Elastic Compute Cloud) - $60/mo
- **GCP** (Google Compute Engine) - $55/mo
- **On-Premise** (Self-hosted) - Custom hardware

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
- SecKey stored encrypted at rest (filesystem encryption)
- Keys loaded into memory only during operations
- No SecKey export API (architectural constraint)
- TLS for all network communications
- Token-based authentication per request

### 2. MCP Tools (API)

**`get_public_key()`**
- Returns: EncKey, EvalKey, MetadataKey (JSON bundle)
- Used by: Team members (one-time at startup)
- Auth: Required (validates token)
- Rate Limit: None (lightweight operation)

**`decrypt_scores()`**
- Input: Encrypted search results blob from enVector Cloud
- Returns: Top-K decrypted scores with indices
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

### Search Query (Runtime)

```
User: "What decisions did we make about database?"
    │
    ▼
AI Agent (Claude/Gemini/Codex)
    │
    ├── 1. Generate query embedding [0.2, 0.4, 0.4, ...]
    │
    ▼
Rune Client
    │
    ├── 2. Encrypt query with EncKey (FHE encryption)
    ├── 3. Submit to enVector Cloud API
    │
    ▼
enVector Cloud
    │
    ├── 4. FHE search on encrypted vectors
    ├── 5. Return encrypted Top-K results
    │
    ▼
Rune Client
    │
    ├── 6. Call decrypt_scores(encrypted_blob) → Vault MCP
    │
    ▼
Vault MCP
    │
    ├── 7. Validate token
    ├── 8. Decrypt with SecKey (FHE decryption)
    ├── 9. Apply Top-K filtering (max 10 results)
    ├── 10. Return [{index: 42, score: 0.95}, ...]
    │
    ▼
Rune Client
    │
    ├── 11. Fetch context metadata from enVector Cloud
    ├── 12. Synthesize answer for user
    │
    ▼
AI Agent → User: "In Q2 2024, team chose PostgreSQL for JSON support..."
```

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
4. **Key Theft**: SecKey extraction → architectural isolation (no export API)

**Threats Not Mitigated** (out of scope):
- Vault VM compromise (admin responsibility: use secure cloud, enable disk encryption)
- Team member laptop compromise (user responsibility: secure devices)
- Token leakage (admin responsibility: rotate tokens, use secure distribution)

### Key Isolation Strategy

**Why SecKey Never Leaves Vault**:
- **Principle**: Decryption capability = highest privilege
- **Constraint**: Only Vault MCP has SecKey, no export API
- **Benefit**: Even if client compromised, attacker cannot decrypt historical data

**Key Distribution**:
```
SecKey:  Vault only (generated on deployment, never exported)
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

## Performance Characteristics

### Latency Breakdown

| Operation | Latency | Notes |
|-----------|---------|-------|
| get_public_key() | ~5ms | Read from disk, serialize JSON |
| decrypt_scores() | ~50ms | FHE decryption (dim=2048, 10 results) |
| Token validation | <1ms | Hash table lookup |
| TLS handshake | ~20ms | First request only (reused connections) |

**Total Search Latency**:
- Client → enVector Cloud: ~100ms (network + FHE search)
- enVector → Vault: ~50ms (decryption)
- **End-to-end**: ~150ms (95th percentile)

### Throughput

| Workload | Throughput | Hardware |
|----------|------------|----------|
| Light (10 queries/min) | 2 OCPU, 8GB RAM | OCI VM.Standard.E4.Flex |
| Medium (100 queries/min) | 4 OCPU, 16GB RAM | Scale up |
| Heavy (1000 queries/min) | 3x instances + LB | Scale out |

### Resource Usage

**Typical Team** (10 members, 100 queries/day):
- CPU: 10-20% average
- Memory: 2GB (FHE keys loaded)
- Disk: 1GB (keys + logs)
- Network: 10GB/month

## Operational Considerations

### Backup & Recovery

**Critical Assets**:
- `/vault_keys/SecKey.json` - **MUST backup** (cannot regenerate)
- `/vault_keys/EncKey.json` - Regenerable from SecKey
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
