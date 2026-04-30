# Rune-Vault Architecture

## System Overview

Rune-Vault is the **infrastructure backbone** for team-shared FHE-encrypted organizational memory. It manages cryptographic keys, authenticates team members, and provides decryption services for encrypted search results.

### Core Responsibilities

1. **Key Management**: Generate, store, and protect FHE keys (secret key isolation)
2. **Decryption Service**: Decrypt search results from enVector Cloud
3. **Authentication**: Validate team member access via tokens
4. **Access Control**: Per-user RBAC with role-based top_k limits, scope enforcement, and rate limiting
5. **Audit Logging**: Structured JSON logs for compliance and debugging

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
                            │  │  Auth & Audit        │  │
                            │  │  - Token validation  │  │
                            │  │  - Audit logging     │  │
                            │  └──────────────────────┘  │
                            └────────────────────────────┘
```

**Key**: Agents never contact Vault directly. The envector-mcp-server's
`remember` tool orchestrates the Vault decryption call as part of its
3-step pipeline. Secret key never leaves Vault.

## Port Summary

| Endpoint | Protocol | Purpose | Exposure |
|----------|----------|---------|----------|
| `:50051` | gRPC + TLS | Vault service, health check, reflection | Public (team members) |
| `/opt/runevault/admin.sock` | Unix domain socket (mode 0600) | Admin token/role CRUD + status | Local only — `runevault` CLI |

## Component Details

### 1. Rune-Vault Server

**Purpose**: Centralized key management and decryption service for a team

**Deployment Options**:
- **OCI** (Oracle Cloud Infrastructure)
- **AWS** (Elastic Compute Cloud)
- **GCP** (Google Compute Engine)
- **On-Premise** (Self-hosted)

**Runtime**:
- Single-binary Go gRPC daemon (`runevault`) — no runtime dependencies beyond TLS
- gRPC server on port 50051 (used by envector-mcp-server)
- gRPC health check via `grpc.health.v1` protocol
- Admin Unix domain socket at `/opt/runevault/admin.sock` (mode 0600, vault-user owned)
- Registered as a native systemd unit (`runevault.service`) on Linux or a launchd job (`com.cryptolabinc.runevault`) on macOS

**Key Storage** (`/opt/runevault/vault-keys/<key-id>/`, default `<key-id>` = `vault-key`):
```
/opt/runevault/vault-keys/vault-key/
├── EncKey.json      # Public encryption key (distributed to agents)
├── EvalKey.json     # Public evaluation key (for FHE operations)
└── SecKey.json      # Secret decryption key (NEVER leaves Vault)
```

Keys are auto-generated on first startup by `EnsureVault` (in `vault/internal/server/ensure_vault.go`).

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
- Interceptor chain: validation (protovalidate + runtime checks) → auth/RBAC → audit
- gRPC reflection enabled (for grpcurl discovery)
- gRPC health checking (`grpc.health.v1`) enabled
- TLS required by default (`server.grpc.tls.disable: true` is dev only — never in production)

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

**Token Format**: `evt_` prefix + 32 hex characters (total 36 chars), generated from `crypto/rand`.
- Example: `evt_a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6`
- Proto-level validation enforces exactly 36 characters.

**Per-User RBAC** (managed by the `tokens` package):
- Each user gets their own token assigned to a role.
- Validation returns the matched user and role.
- Checks: token existence, expiration, rate limit (per-user sliding window).
- Scope is checked separately per gRPC method.

**Default Roles:**

| Role | Scope | top_k | Rate Limit |
|------|-------|-------|------------|
| admin | get_public_key, decrypt_scores, decrypt_metadata, manage_tokens | 50 | 150/60s |
| member | get_public_key, decrypt_scores, decrypt_metadata | 10 | 30/60s |

Custom roles can be created via `runevault role create`.

**Token Lifecycle:**
- Issue: `runevault token issue --user alice --role member --expires 90d`
- Rotate: `runevault token rotate --user alice` (atomic revoke + reissue) or `--all`
- Revoke: `runevault token revoke --user alice`
- Persistence: atomic YAML writes to the files referenced by `tokens.tokens_file` and `tokens.roles_file` in `runevault.conf` (defaults: `/opt/runevault/configs/{tokens,roles}.yml`).

**Configuration Source**: `runevault.conf` (YAML) is the single source of truth — no env-var fallback or migration helper. Lookup order:
1. `--config <path>` CLI flag
2. `/opt/runevault/configs/runevault.conf`
3. `./runevault.conf` (cwd, dev only)

Secret YAML fields (`tokens.team_secret`, `envector.api_key`) accept a sibling `*_file` key for KMS-backed deployments.

### 4. Admin Socket & CLI

**Admin Socket** (`vault/internal/server/admin.go`):
- Unix domain socket at `/opt/runevault/admin.sock` (mode 0600, vault-user owned)
- Filesystem permissions are the only authorization gate; never expose externally
- Used by the `runevault` CLI and by the daemon's lifecycle hooks (e.g. `ErrRestartRequested` after token rotation)

**CLI** (`runevault`):

| Command | Purpose |
|---------|---------|
| `runevault status` | Daemon health and socket liveness |
| `runevault logs` | Tail audit log output |
| `runevault token issue --user <name> --role <role> [--expires 90d]` | Issue a new per-user token |
| `runevault token list` | List issued tokens |
| `runevault token rotate --user <name>` / `--all` | Atomic revoke + reissue |
| `runevault token revoke --user <name>` | Revoke a token |
| `runevault role list` | List configured roles |
| `runevault role create --name <name> --scope a,b,c --top-k N --rate-limit N/Ts` | Create a custom role |
| `runevault role update --name <name> [--scope] [--top-k] [--rate-limit]` | Update an existing role |
| `runevault role delete --name <name>` | Delete a role |
| `runevault version` | Print build version (works without daemon or socket) |

The `daemon start` subcommand is invoked by systemd / launchd; operators
control lifecycle via `systemctl … runevault` (Linux) or
`launchctl … system/com.cryptolabinc.runevault` (macOS) rather than directly.

### 5. Input Validation

Two-layer validation runs as a gRPC interceptor before requests reach business logic:

- **Layer 1: protovalidate** -- Enforces `.proto` annotation constraints (field length, int range, repeated item rules)
- **Layer 2: Runtime checks** -- Control character rejection, whitespace validation (not expressible in proto annotations)

Non-Vault methods (health check, reflection) pass through untouched.

### 6. Per-Agent Metadata Encryption

Each agent gets a unique 32-byte AES-256 DEK (Data Encryption Key):

```
DEK = HKDF-SHA256(key=tokens.team_secret, info=agent_id)
agent_id = SHA256(token)[:32]
```

- DEK is distributed to the agent via the `GetPublicKey()` response (`agent_dek` field)
- Metadata is encrypted client-side with the agent-specific DEK
- Vault re-derives the DEK from team secret + agent_id to decrypt
- Ensures one agent cannot decrypt another agent's metadata even if both are on the same team

### 7. Audit Logging

Structured JSON logging for all gRPC operations (`vault/internal/server/audit.go`):

- One JSON line per request: timestamp, user_id, method, top_k, result_count, status, source_ip, latency_ms, error
- Source IP extracted from the gRPC peer context
- File output uses `lumberjack` for size-based rotation

**Configuration** in `runevault.conf`:

```yaml
audit:
  mode: file+stdout         # one of: "" (disabled), file, stdout, file+stdout
  path: /opt/runevault/logs/audit.log
```

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
- Firewall rules (allow gRPC 50051)
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

**Layer 4: Audit**
- Structured JSON audit logs (compliance reporting)
- Per-request logging: user, method, latency, status, source IP

## Deployment Architecture

### Cloud Deployment (Terraform)

`install.sh --target <aws|gcp|oci>` wraps Terraform end-to-end:
preflight checks → `terraform apply` → cloud-init bootstrap → CA-cert SCP
poll → remote `install.sh` execution.

```
Terraform Configuration
    │
    ├── deployment/aws/main.tf    # Amazon Web Services
    ├── deployment/gcp/main.tf    # Google Cloud Platform
    └── deployment/oci/main.tf    # Oracle Cloud Infrastructure
        │
        ▼
Cloud Resources Created
    │
    ├── Compute Instance (VM)
    │   ├── OS: Ubuntu 24.04 LTS
    │   └── Software (installed via cloud-init startup script):
    │       ├── runevault binary (Sigstore-verified)
    │       └── runevault.service (systemd) registered
    │
    ├── Networking
    │   ├── Public IP address
    │   └── Security group / list / firewall rule (allow 50051/gRPC)
    │
    ├── Storage
    │   └── /opt/runevault/vault-keys/<key-id>/  (FHE keys)
    │
    └── Audit Logging
        └── /opt/runevault/logs/audit.log
```

Common Terraform variables across all CSPs: `team_name`, `tls_mode`,
`envector_endpoint`, `envector_api_key`, `runevault_version`,
`public_key`, `region`. CSP-specific: `instance_type` (AWS),
`project_id` / `zone` / `machine_type` (GCP), `oci_profile` /
`compartment_id` (OCI). Output: `vault_public_ip`.

Horizontal scaling and multi-instance HA are not currently supported.
For higher capacity, re-provision with a larger VM shape via your cloud
provider.

## Operational Considerations

### Backup & Recovery

**Critical Assets**:
- `/opt/runevault/vault-keys/<key-id>/SecKey.json` — **MUST backup** (cannot regenerate)
- `tokens.team_secret` from `runevault.conf` — **MUST backup** (needed for DEK re-derivation)
- Per-user tokens — rotatable via `runevault token rotate`

**Backup Strategy**:
```bash
# Manually back up vault keys (run on the VM)
sudo tar czf vault-keys_backup_$(date +%Y-%m-%d).tar.gz -C /opt/runevault vault-keys/

# Also archive runevault.conf or at minimum the tokens.team_secret value
# Store in: offline media, a different cloud provider, or a password manager
```

**Recovery Procedure**:
```bash
# 1. Re-provision a fresh VM via the installer
sudo bash install.sh --target <aws|gcp|oci>

# 2. Stop the daemon before restoring keys
sudo systemctl stop runevault

# 3. Restore vault-keys and team_secret
sudo tar xzf vault-keys_backup_YYYY-MM-DD.tar.gz -C /opt/runevault
# Edit /opt/runevault/configs/runevault.conf and restore tokens.team_secret

# 4. Bring the daemon back up
sudo systemctl start runevault
# Team members continue without reconfiguration.
```

### Token Rotation

```bash
# Rotate a single user's token
runevault token rotate --user alice

# Rotate all tokens
runevault token rotate --all

# Distribute new tokens to team members via a secure channel
```

### Scaling Strategy

Re-provision with a larger VM shape via your cloud provider's console or
by editing the relevant `instance_type` (AWS) / `machine_type` (GCP) /
shape configuration (OCI) and re-running `terraform apply` from your
install directory.

When to scale:
- CPU >80% sustained
- Latency P95 >200ms
- Error rate >1% (investigate first — usually a config issue, not scale)

## Module Reference

| Package | Purpose |
|---------|---------|
| `vault/cmd` | Binary entry point — wires Cobra root command and runs `Execute()` |
| `vault/internal/commands` | CLI subcommands (`daemon`, `token`, `role`, `status`, `logs`, `version`) and admin-socket client |
| `vault/internal/server` | gRPC server, config loader, audit logger, admin UDS, `EnsureVault` startup hook, interceptors |
| `vault/internal/tokens` | Per-user RBAC store: tokens, roles, validation, rate limiting, YAML persistence |
| `vault/internal/crypto` | FHE key management + HKDF/AES wrappers around `envector-go-sdk` |
| `vault/internal/tests` | E2E tests gated by build tag `e2e` (decrypt pipeline + CLI smoke) |
| `vault/pkg/vaultpb` | Generated gRPC stubs from `vault/proto/*.proto` |

## Troubleshooting

### Issue: High Latency

**Symptoms**: `DecryptScores` taking >200ms

**Diagnosis**:
```bash
# Check Vault CPU on the server
ssh ubuntu@<vault-host>     # or ec2-user@... / opc@... depending on CSP
top

# Tail the audit log for latency
sudo tail -20 /opt/runevault/logs/audit.log
# Or use the CLI from the host:
runevault logs
```

**Solutions**:
- CPU bottleneck → Re-provision with a larger VM shape
- Large Top-K → Reduce max results (or tighten role `top_k`)
- High dimension → Acceptable (dim=1024 is standard)

### Issue: Authentication Failures

**Symptoms**: Clients can't connect, UNAUTHENTICATED error

**Diagnosis**:
```bash
# Verify the daemon is up
runevault status

# Inspect server logs for denied requests
sudo journalctl -u runevault | grep -i "denied\|unauthenticated"
```

**Solutions**:
- Wrong token → Re-share the correct token
- Token rotated → Distribute the new token to all team members
- Token expired → Issue a fresh token via `runevault token issue`
- Rate limited → Wait for the window to reset, or adjust the role's `rate_limit`
- Firewall → Check the security group allows 50051 from team IPs

### Issue: Vault Crashed

**Symptoms**: Health check fails, daemon not responsive

**Diagnosis**:
```bash
# Linux
sudo systemctl status runevault
sudo journalctl -u runevault -n 100

# macOS
sudo launchctl print system/com.cryptolabinc.runevault
sudo log show --predicate 'process == "runevault"' --last 10m
```

**Solutions**:
- OOM killer → Increase VM memory
- Disk full → Rotate logs (`lumberjack` handles size-based rotation, but free disk first)
- Crashed process → `sudo systemctl restart runevault` (Linux) / `sudo launchctl kickstart -k system/com.cryptolabinc.runevault` (macOS)
- Persistent crash → Re-provision with `install.sh --uninstall` then `install.sh --target <provider>`, restoring `vault-keys/` from backup before first start

## Next Steps

- Deploy your first Vault: [Quick Start](../README.md#quick-start)
- Contributing: [CONTRIBUTING.md](../CONTRIBUTING.md)
