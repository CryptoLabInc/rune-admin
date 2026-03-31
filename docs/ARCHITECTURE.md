# Rune-Vault Architecture

## System Overview

Rune-Vault is the **infrastructure backbone** for team-shared FHE-encrypted organizational memory. It manages cryptographic keys, authenticates team members, and provides decryption services for encrypted search results.

### Core Responsibilities

1. **Key Management**: Generate, store, and protect FHE keys (secret key isolation)
2. **Decryption Service**: Decrypt search results from enVector Cloud
3. **Authentication**: Validate team member access via tokens
4. **Access Control**: Per-user RBAC with role-based top_k limits, scope enforcement, and rate limiting
5. **Monitoring**: Track usage, performance, and security metrics

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
  │ enVector Cloud(SaaS) │  │        Rune-Vault           │
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
                            │  │  gRPC Service (:50051)│  │
                            │  │  - GetPublicKey()    │  │
                            │  │  - DecryptScores()   │  │
                            │  │  - DecryptMetadata() │  │
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

## Port Summary

| Port | Protocol | Purpose | Exposure |
|------|----------|---------|----------|
| 50051 | gRPC + TLS | Vault service, health check, reflection | Public (team members) |
| 9090 | HTTP | Health, metrics, status | Host-only (127.0.0.1 in Docker) |
| 8081 | HTTP | Admin token/role CRUD | Container-internal only |

## Component Details

### 1. Rune-Vault Server

**Purpose**: Centralized key management and decryption service for a team

**Deployment Options**:
- **OCI** (Oracle Cloud Infrastructure)
- **AWS** (Elastic Compute Cloud)
- **GCP** (Google Compute Engine)
- **On-Premise** (Self-hosted)

**Runtime**:
- Python 3.12 gRPC server
- gRPC server on port 50051 (used by envector-mcp-server)
- HTTP health/metrics endpoint on port 9090
- Internal admin HTTP API on port 8081 (container-local only)
- Prometheus metrics exporter
- System monitoring (psutil)

**Key Storage** (`vault_keys/vault-key/`):
```
vault_keys/vault-key/
├── EncKey.json      # Public encryption key (distributed to agents)
├── EvalKey.json     # Public evaluation key (for FHE operations)
└── SecKey.json      # Secret decryption key (NEVER leaves Vault)
```

Keys are auto-generated on first startup via `ensure_vault()`.

**Security Properties**:
- Secret key stored encrypted at rest (filesystem encryption)
- Keys loaded into memory only during operations
- No secret key export API (architectural constraint)
- TLS for all network communications
- Token-based authentication per request

### 2. gRPC Service (API)

Defined in `proto/vault_service.proto` (`rune.vault.v1.VaultService`).

**Server Configuration**:
- Max message size: 256 MB (for EvalKey transfer)
- Thread pool: 4 workers
- Interceptor chain: `ValidationInterceptor` (protovalidate + runtime checks)
- gRPC reflection enabled (for grpcurl discovery)
- gRPC health checking (`grpc.health.v1`) enabled
- TLS required by default (disable via `VAULT_TLS_DISABLE=true`)

**`GetPublicKey()`**
- Returns: JSON bundle containing EncKey, EvalKey, index_name, key_id, agent_id, agent_dek (per-user derived encryption key)
- Used by: envector-mcp-server at startup
- Auth: Required (validates token + scope check)

**`DecryptScores()`**
- Input: Result ciphertext from encrypted similarity search (base64-serialized)
- Returns: Top-K typed `ScoreEntry` messages (shard_idx, row_idx, score)
- Used by: envector-mcp-server's `remember` pipeline (per search query)
- Auth: Required (validates token + scope check)
- Policy: Per-role top_k limit (admin: 50, member: 10). Proto constraint: 1-300 range.

**`DecryptMetadata()`**
- Input: List of AES-encrypted metadata blobs. Each blob is JSON `{"a": "<agent_id>", "c": "<base64_ciphertext>"}`.
- Returns: Decrypted metadata (JSON strings)
- Used by: envector-mcp-server's `remember` pipeline
- Auth: Required (validates token + scope check)
- Vault derives the agent's DEK via HKDF-SHA256 from team secret + agent_id.

### 3. Authentication & Access Control

**Token Format**: `evt_` prefix + 32 hex characters (total 36 chars), generated via `secrets.token_hex(16)`.
- Example: `evt_a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6`
- Proto-level validation enforces exactly 36 characters.

**Per-User RBAC** (managed by `TokenStore`):
- Each user gets their own token assigned to a role.
- `validate()` returns `(username, Role)` tuple.
- Checks: token existence, expiration, rate limit (per-user sliding window).
- Scope checked separately per gRPC method.

**Default Roles:**

| Role | Scope | top_k | Rate Limit |
|------|-------|-------|------------|
| admin | get_public_key, decrypt_scores, decrypt_metadata, manage_tokens | 50 | 150/60s |
| member | get_public_key, decrypt_scores, decrypt_metadata | 10 | 30/60s |

Custom roles can be created via the Admin API or CLI.

**Token Lifecycle:**
- Issue: `runevault token issue --user alice --role member --expires 90d`
- Rotate: `runevault token rotate --user alice` (atomic revoke + reissue)
- Revoke: `runevault token revoke --user alice`
- Persistence: async YAML writes to `vault-tokens.yml` / `vault-roles.yml` (atomic via temp file + `os.replace`)

**Configuration Priority** (at startup):
1. YAML config files (`vault-roles.yml`, `vault-tokens.yml`)
2. Legacy env var (`VAULT_TOKENS`)
3. Demo mode (auto-generated demo token)

### 4. Admin Server & CLI

**Admin Server** (`admin_server.py`):
- HTTP on `127.0.0.1:8081` (not exposed via Docker; access via `docker exec`)
- No authentication (protected by container isolation)
- REST API for token and role CRUD operations

| Method | Path | Purpose |
|--------|------|---------|
| GET | /tokens | List all tokens |
| POST | /tokens | Issue new token |
| DELETE | /tokens/{user} | Revoke token |
| POST | /tokens/{user}/rotate | Rotate single token |
| POST | /tokens/_rotate_all | Rotate all tokens |
| GET | /roles | List all roles |
| POST | /roles | Create role |
| PUT | /roles/{name} | Update role |
| DELETE | /roles/{name} | Delete role |

**CLI** (`vault_admin_cli.py` / `runevault`):
- Wraps the Admin HTTP API for operator convenience
- Available inside the container

### 5. Input Validation

Two-layer validation runs as a gRPC interceptor before requests reach business logic:

- **Layer 1: protovalidate** -- Enforces `.proto` annotation constraints (field length, int range, repeated item rules)
- **Layer 2: Runtime checks** -- Control character rejection, whitespace validation (not expressible in proto annotations)

Non-Vault methods (health check, reflection) pass through untouched.

### 6. Per-Agent Metadata Encryption

Each agent gets a unique 32-byte AES-256 DEK (Data Encryption Key):

```
DEK = HKDF-SHA256(key=VAULT_TEAM_SECRET, info=agent_id)
agent_id = SHA256(token)[:32]
```

- DEK is distributed to the agent via the `GetPublicKey()` response (`agent_dek` field)
- Metadata is encrypted client-side with the agent-specific DEK
- Vault re-derives the DEK from team secret + agent_id to decrypt
- Ensures one agent cannot decrypt another agent's metadata even if both are on the same team

### 7. Monitoring & Observability

**Prometheus Metrics** (exposed at `:9090/metrics`):
```
# Request tracking (all gRPC methods)
vault_requests_total{method, endpoint, status, user}
vault_request_duration_seconds{method, endpoint}

# Decryption operations
vault_decryption_operations_total{status}
vault_decryption_duration_seconds

# Key access
vault_key_access_total{key_type, status}

# System gauges
vault_health_status          (1=healthy, 0=unhealthy)
vault_cpu_usage_percent
vault_memory_usage_bytes
vault_uptime_seconds
```

**HTTP Endpoints** (port 9090):

| Endpoint | Purpose |
|----------|---------|
| `/health` | Full health check (sub-checks: keys, memory, cpu, disk) |
| `/health/ready` | Readiness probe (keys accessible?) |
| `/health/live` | Liveness probe (always 200 if process running) |
| `/metrics` | Prometheus metrics (text format) |
| `/status` | Service status with version, uptime, resource usage |

**Health Check** (`/health`):
- Runs sub-checks for keys, memory, cpu, disk
- Each check returns: `{status: healthy|degraded|unhealthy, message}`
- Overall status: unhealthy if any check is unhealthy, degraded if any degraded
- Thresholds: >80% = degraded, >90% = unhealthy (memory, cpu, disk)

**Grafana Dashboards**:
- See `deployment/monitoring/grafana-dashboard.json` for templates
- Pre-configured alerts in `deployment/monitoring/prometheus-alerts.yml`

### 8. Audit Logging

Structured JSON logging for all gRPC operations (`audit.py`):

- One JSON line per request: timestamp, user_id, method, top_k, result_count, status, source_ip, latency_ms, error
- Source IP extracted from gRPC `context.peer()`

**Configuration** (via `VAULT_AUDIT_LOG` env var):

| Value | Behavior |
|-------|----------|
| *(empty)* | Disabled |
| `file` | `/var/log/rune-vault/audit.log` (daily rotation, 30-day retention) |
| `file:/path` | Custom file path |
| `stdout` | JSON lines to stdout (for cloud log aggregators) |
| `file+stdout` | Both |

## Data Flow

### Client Initialization (One-Time)

```
Team Member's Laptop
    │
    ├── 1. Install Rune from Claude Marketplace (github.com/CryptoLabInc/rune)
    ├── 2. Configure Vault Endpoint + Token
    │
    ▼
Rune Startup
    │
    ├── 3. Call GetPublicKey() → Vault (gRPC :50051)
    │
    ▼
Vault (gRPC)
    │
    ├── 4. Validate token (returns username + role)
    ├── 5. Read EncKey.json, EvalKey.json
    ├── 6. Derive agent DEK via HKDF
    ├── 7. Return key bundle (EncKey, EvalKey, index_name, key_id, agent_id, agent_dek)
    │
    ▼
Rune Client
    │
    └── 8. Store keys and agent DEK locally; use EncKey for encryption, agent DEK for metadata encryption
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
    ├── 4. Call Vault: DecryptScores(token, ciphertext, top_k) via gRPC
    │
    ▼
Vault (gRPC — secret key holder)
    │
    ├── 5. Validate token (returns username + role)
    ├── 6. Decrypt result ciphertext with secret key → similarity values
    ├── 7. Select top-k (per-role limit; admin: 50, member: 10)
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
- **Constraint**: Only Vault has secret key, no export API
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
- Firewall rules (allow gRPC 50051; monitoring 9090 host-only)
- Optional: VPN for extra isolation

**Layer 2: Authentication**
- Token validation on every request
- Rate limiting (per-user sliding window)
- Audit logging (track who accesses what)

**Layer 3: Cryptography**
- FHE encryption (data encrypted at source)
- Keys encrypted at rest (filesystem encryption)
- Secure key generation (crypto-safe randomness)
- Per-agent metadata DEKs (agent isolation)

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
    │       ├── Python 3.12
    │       ├── pyenvector (FHE SDK)
    │       └── Prometheus exporter
    │
    ├── Networking
    │   ├── Public IP address
    │   ├── Security group (allow 50051/gRPC; 9090 host-only)
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
- `/vault_keys/vault-key/SecKey.json` - **MUST backup** (cannot regenerate)
- `VAULT_TEAM_SECRET` - **MUST backup** (needed for DEK re-derivation)
- Vault token - Rotatable via CLI

**Backup Strategy**:
```bash
# Manually back up vault keys
tar czf vault_keys_backup_$(date +%Y-%m-%d).tar.gz vault/vault_keys/

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

### Token Rotation

```bash
# Rotate a single user's token
runevault token rotate --user alice

# Rotate all tokens
runevault token rotate --all

# Distribute new tokens to team members via secure channel
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
- Real-time: Grafana (`deployment/monitoring/grafana-dashboard.json`)
- Historical: Prometheus (`deployment/monitoring/prometheus-alerts.yml`)

## Module Reference

| Module | Purpose |
|--------|---------|
| `vault_core.py` | Core business logic: key management, decryption, DEK derivation |
| `vault_grpc_server.py` | gRPC server, TLS, entrypoint, orchestrates all subsystems |
| `token_store.py` | Per-user RBAC: Token/Role dataclasses, validation, rate limiting, YAML persistence |
| `admin_server.py` | Internal HTTP admin API for token/role CRUD |
| `validation_interceptor.py` | gRPC interceptor: protovalidate + runtime input checks |
| `request_validator.py` | Runtime validation rules (control chars, whitespace) |
| `audit.py` | Structured JSON audit logging with file rotation |
| `monitoring.py` | Health checks, Prometheus metrics, /status endpoint |
| `vault_admin_cli.py` | CLI for token/role management (`runevault` command) |
| `verify_crypto_flow.py` | Crypto pipeline verification script |

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
- Large Top-K → Reduce max results
- High dimension → Acceptable (dim=1024 is standard)

### Issue: Authentication Failures

**Symptoms**: Clients can't connect, UNAUTHENTICATED error

**Diagnosis**:
```bash
# Check token is correct
echo $RUNEVAULT_TOKEN

# Verify Vault sees requests
ssh admin@vault-yourteam.oci.envector.io
sudo journalctl -u vault | grep "denied"
```

**Solutions**:
- Wrong token → Re-share correct token
- Token rotated → Distribute new token to all team members
- Token expired → Issue new token via `runevault token issue`
- Rate limited → Wait for window reset or adjust role rate_limit
- Firewall → Check security group allows 50051 from team IPs

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
- Load testing: `scripts/load-test.sh`
- Monitoring setup: `deployment/monitoring/`
