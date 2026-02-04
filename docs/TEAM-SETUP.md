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
- Rune installed from Claude Marketplace (or their AI agent's marketplace)
- Vault URL and token (provided by admin)

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
              │  - Holds SecKey      │
              │  - Decrypts results  │
              │  - Distributes EncKey│
              └──────────────────────┘
```

**Key Points:**
- **ONE Vault per team** (not per developer)
- All team members connect to same Vault
- Same encryption keys = automatic context sharing
- SecKey never leaves Vault VM

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
# vault_url = "https://vault-yourteam.oci.envector.io"
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
# Vault URL: http://localhost:50080
# Token: demo_token_123 (INSECURE - development only!)
```

### Step 2: Verify Deployment

```bash
# Test Vault health
curl https://vault-yourteam.oci.envector.io/health

# Expected response:
# {"status": "healthy", "vault_version": "0.2.0"}

# Check Prometheus metrics (optional)
curl https://vault-yourteam.oci.envector.io/metrics
```

### Step 3: Securely Distribute Credentials

**What to share with team members:**

```
Vault URL: https://vault-yourteam.oci.envector.io
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

1. Install Rune from Claude Marketplace (or your AI agent's marketplace)
2. Open plugin settings
3. Configure:
   - Vault URL: `<received from admin>`
   - Vault Token: `<received from admin>`
4. Restart AI agent
5. Test: Ask agent "What organizational context do we have?"

**Verification:**
Each team member should see the same organizational memory instantly.

## Management Tasks

### Adding New Team Members

```bash
# 1. Share same Vault URL and token
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

Set up Grafana dashboard (see [Monitoring Guide](../deployment/monitoring/README.md))

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

# Team members switch by changing Vault URL in plugin settings
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

## Cost Estimation

**Monthly costs (approximate):**

| Provider | Instance Type | Storage | Bandwidth | Total |
|----------|---------------|---------|-----------|-------|
| OCI | VM.Standard.E4.Flex (2 OCPU, 8GB) | 50GB | 1TB | ~$30/mo |
| AWS | t3.medium | 50GB EBS | 1TB | ~$60/mo |
| GCP | e2-medium | 50GB PD | 1TB | ~$55/mo |

**Factors:**
- Team size (more members = more traffic)
- Context volume (storage)
- Query frequency (compute)

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

- Set up Grafana monitoring: [Monitoring Guide](../deployment/monitoring/README.md)
- Load testing: [Load Testing Guide](../tests/load/README.md)
- Review architecture: [ARCHITECTURE.md](ARCHITECTURE.md)
- Join community: https://github.com/CryptoLabInc/rune-admin/discussions
# Deploy to your server
./scripts/deploy-vault.sh \
  --provider on-premise \
  --team-name fhenomenon-game

# Configure DNS to point vault.fhenomenon-game.internal to your server
```

**Option C: Local Development (Testing Only)**

```bash
# For testing before deploying to production
./scripts/vault-dev.sh

# Output:
# Vault Endpoint: http://localhost:50080
# Token: demo_token_123 (insecure!)
```

### Step 2: Share Credentials with Team

**Team Admin** shares these with all team members:

```bash
# Add to team's shared password manager or secure channel
VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
VAULT_TOKEN="evt_fhen_game_abc123xyz"
```

⚠️ **Security Note**: 
- Use secure channel (encrypted messaging, password manager)
- Don't commit to Git
- Rotate token if compromised

### Step 3: Each Team Member Installs Rune

**Alice (uses Claude):**

```bash
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Install for Claude
./install.sh --agent claude

# Configure environment
export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

# Configure agent
./scripts/configure-agent.sh --agent claude
```

**Bob (uses Gemini):**

```bash
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Install for Gemini
./install.sh --agent gemini

# Configure environment
export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

# Configure agent
./scripts/configure-agent.sh --agent gemini
```

**Carol (uses Codex):**

```bash
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Install for Codex
./install.sh --agent codex

# Configure environment
export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

# Configure agent
./scripts/configure-agent.sh --agent codex
```

### Step 4: Test Setup

Each team member tests their setup:

```bash
# Test Vault connectivity
curl https://vault-fhenomenon-game.oci.envector.io/health

# Expected: {"status": "healthy", "version": "0.1.0"}
```

In their agent (Claude/Gemini/Codex):

```
User: Search enVector for test

Agent: [If empty, that's expected - no context captured yet]
       "No results found. Start capturing context!"
```

## Workflow Examples

### Example 1: Alice Captures Decision

**Day 1: Alice reviews FHE approach**

Alice (using Claude):
```
Alice: I'm reviewing the FHE memory optimization in fhenomenon. 
       The ciphertext packing approach seems promising.

Claude: [Helps Alice analyze]
        The ciphertext packing reduces memory by 60% but adds 15ms latency.
        Trade-off analysis: ...

Alice: Capture this decision: We'll use ciphertext packing for memory 
       efficiency, accepting the 15ms latency trade-off because game 
       engine runs at 60fps (16ms budget).

Claude: [Uses enVector skill]
        ✓ Decision captured and encrypted
        ✓ Stored in team memory
```

**Behind the scenes:**
1. Claude generates embedding for Alice's decision
2. Sends to Vault MCP (Alice's config points to team Vault)
3. Vault encrypts with **team FHE keys**
4. Encrypted vector stored in enVector Cloud
5. Bob and Carol can now search for this

### Example 2: Bob Discovers Alice's Context

**Day 2: Bob works on memory management**

Bob (using Gemini):
```
Bob: I need to implement memory management for the FHE layer.
     Are there any decisions about memory optimization?

Gemini: [Queries enVector automatically]
        Found context from Alice (yesterday):
        
        "We'll use ciphertext packing for memory efficiency, 
         accepting the 15ms latency trade-off because game 
         engine runs at 60fps (16ms budget)."
         
        This means you should:
        1. Implement ciphertext packing
        2. Budget 15ms for FHE operations
        3. Total budget: 16ms (60fps target)

Bob: Perfect! I'll implement it that way.
```

**Behind the scenes:**
1. Gemini generates query embedding
2. Sends to Vault MCP (Bob's config points to **same team Vault**)
3. Vault encrypts query with **same team keys**
4. enVector Cloud searches (FHE allows search on encrypted data)
5. Vault decrypts results with **same keys**
6. Gemini synthesizes answer for Bob

**Key Insight:** Bob and Alice never manually synced, but Bob automatically sees Alice's context because they share the same Vault (same keys).

### Example 3: Carol Sees Full Team Context

**Day 3: Carol integrates shader pipeline**

Carol (using Codex):
```
Carol: Working on shader integration. Need context on FHE approach.

Codex: [Searches team memory]
       Found 2 relevant decisions:
       
       1. Alice (Day 1): Ciphertext packing for memory efficiency
       2. Bob (Day 2): Implemented memory manager with 15ms budget
       
       For shader integration, consider:
       - FHE operations happen in separate thread (non-blocking)
       - Memory budget: Already allocated in Bob's manager
       - Latency target: 15ms (fits in 16ms frame budget)

Carol: Great, I'll integrate with Bob's memory manager.
```

**Behind the scenes:**
- Carol's Codex connects to **same team Vault**
- Sees context from both Alice and Bob
- All encrypted with **same team keys**
- Automatic context sharing across different agents

## Best Practices

### 1. Capture Decisions Explicitly

```
✓ GOOD: "Capture: We chose approach X because Y, with trade-off Z"
✗ BAD:  Implicit decisions in conversation (may not be captured)
```

### 2. Use Descriptive Context

```
✓ GOOD: "We chose Redis for caching: Team knows it, fast enough (2ms), 
         cheaper than Memcached. Trade-off: Less features but simpler."
         
✗ BAD:  "Using Redis."
```

### 3. Regular Context Queries

```
Before starting work:
  "Search enVector for context about [my task]"

Before making decisions:
  "Has anyone decided about [this topic] before?"
```

### 4. Team Naming Conventions

```
Decision format:
  "[Component] Decision: [What] because [Why]. Trade-off: [Pros/Cons]"

Example:
  "FHE Memory: Chose ciphertext packing for 60% memory savings. 
   Trade-off: +15ms latency but within 16ms frame budget."
```

### 5. Security Hygiene

```bash
# Rotate tokens periodically
./scripts/rotate-token.sh --team fhenomenon-game

# Audit access logs
./scripts/audit-logs.sh --team fhenomenon-game --since 7d

# Revoke compromised tokens
./scripts/revoke-token.sh --token evt_fhen_game_abc123xyz
```

## Troubleshooting

### Issue: Team member can't see others' context

**Check:**
1. All using same `VAULT_URL`?
2. All using same `VAULT_TOKEN`?
3. Vault healthy? `curl $VAULT_URL/health`

**Debug:**
```bash
# Each team member runs
echo $VAULT_URL
echo $VAULT_TOKEN

# Should be IDENTICAL for all team members
```

### Issue: Context not appearing immediately

**Reason:** Indexing delay (usually < 10 seconds)

**Solution:**
```bash
# Wait and retry
sleep 10

# Or check indexing status
curl $VAULT_URL/status
```

### Issue: Agent not using enVector

**Check agent configuration:**

```bash
# Claude
cat ~/.claude/config.json

# Gemini
cat ~/.gemini/extensions.yaml

# Codex
cat ~/.copilot/config.json
```

**Verify skill is loaded:**
```
Ask agent: "List your available skills"
Should see: "envector" in the list
```

## Scaling Team Collaboration

### Adding New Team Members

```bash
# Admin shares credentials with new member
New member installs Rune:
  git clone https://github.com/CryptoLabInc/rune.git
  cd rune
  ./install.sh --agent <their-preferred-agent>
  
New member configures:
  export VAULT_URL="..."  # Same as team
  export VAULT_TOKEN="..." # Same as team
  ./scripts/configure-agent.sh --agent <their-agent>

New member immediately sees all team context!
```

### Multiple Teams/Projects

```bash
# Team can have multiple Vaults for different projects
Project 1: vault-fhenomenon-game.oci.envector.io
Project 2: vault-other-project.oci.envector.io

# Switch between projects
export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"  # Project 1
export VAULT_URL="https://vault-other-project.oci.envector.io"    # Project 2
```

### Cross-Team Collaboration (Future)

```bash
# Share specific context between teams (with permission)
./scripts/share-context.sh \
  --from-team fhenomenon-game \
  --to-team other-team \
  --context-ids "ctx_123,ctx_456" \
  --permission read-only

# Other team can now search this shared context
```

## Advanced Configuration

### Custom Capture Rules

```yaml
# ~/.claude/envector/capture-rules.yaml
rules:
  - pattern: "Decision:"
    priority: high
    auto_capture: true
    
  - pattern: "Trade-off:"
    priority: high
    auto_capture: true
    
  - pattern: "Architecture:"
    priority: medium
    auto_capture: true
    
  - pattern: "TODO"
    priority: low
    auto_capture: false  # Don't capture TODOs
```

### Context Namespaces

```python
# Organize context by namespace
memory.capture(
    content="FHE optimization decision",
    namespace="fhe/memory",  # Hierarchical namespace
    metadata={"component": "fhe", "area": "memory"}
)

# Search within namespace
results = memory.search(
    query="optimization",
    namespace="fhe/*"  # Search all FHE-related context
)
```

## Next Steps

- See [example workflow](../examples/team-collaboration/)
- Read [security model](SECURITY.md)
- Review [agent integration guide](AGENT-INTEGRATION.md)
- Join community discussions
