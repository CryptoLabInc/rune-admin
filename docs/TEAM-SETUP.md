# Team Collaboration Setup Guide

## Overview

This guide shows how to set up HiveMinded for **team collaboration** where multiple developers work on a confidential project and share AI agent context seamlessly.

## Use Case: Confidential Game Engine Port

**Scenario:**
- 3 developers forking [fhenomenon](https://github.com/zotanika/fhenomenon)
- Porting to game engine (confidential project)
- Remote collaboration
- Each developer uses different AI agent (Claude/Gemini/Codex)
- Want seamless context sharing without manual sync

## Prerequisites

Before setting up team collaboration, ensure:

1. **All team members have signed up for enVector Cloud** at [https://envector.io](https://envector.io)
2. **Obtain organization API credentials** (`org-id`, `api-key`) from the enVector Cloud dashboard

## Architecture

```
┌─────────────────────────────────────────────────────┐
│   enVector Cloud (https://envector.io - Required)   │
│         Stores encrypted vectors only               │
└─────────────────────────────────────────────────────┘
          ▲               ▲               ▲
          │ encrypted     │ encrypted     │ encrypted
┌─────────┴────┐  ┌───────┴──────┐  ┌────┴─────────┐
│   Alice      │  │     Bob      │  │    Carol     │
│   (Claude)   │  │   (Gemini)   │  │   (Codex)    │
│              │  │              │  │              │
│ Monitor Agent│  │ Monitor Agent│  │ Monitor Agent│
│      ↓       │  │      ↓       │  │      ↓       │
│  MCP Client  │  │  MCP Client  │  │  MCP Client  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       └─────────────────┴─────────────────┘
                         │
                         ▼
              ┌──────────────────────┐
              │   Team Vault (Keys)  │
              │  OCI/AWS/GCP or      │
              │  Self-hosted         │
              └──────────────────────┘
```

**Key Points:**
- **ONE Vault per team** (not per developer)
- All team members connect to same Vault
- Same encryption keys = automatic context sharing
- No manual synchronization needed

## Step-by-Step Setup

### Step 1: Team Admin Deploys Vault

**Option A: Managed Cloud (Recommended)**

```bash
# Choose your cloud provider
cd HiveMinded

# Deploy to OCI (Oracle Cloud)
./scripts/deploy-vault.sh \
  --provider oci \
  --team-name fhenomenon-game \
  --region us-ashburn-1

# OR deploy to AWS
./scripts/deploy-vault.sh \
  --provider aws \
  --team-name fhenomenon-game \
  --region us-east-1

# OR deploy to GCP
./scripts/deploy-vault.sh \
  --provider gcp \
  --team-name fhenomenon-game \
  --region us-central1
```

**Output:**
```
✓ Vault deployed successfully!

Vault Endpoint: https://vault-fhenomenon-game.oci.envector.io
Team Token: evt_fhen_game_abc123xyz

Share these credentials with your team:
  export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
  export VAULT_TOKEN="evt_fhen_game_abc123xyz"
```

**Option B: Self-Hosted (On-Premise)**

```bash
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

### Step 3: Each Team Member Installs HiveMinded

**Alice (uses Claude):**

```bash
git clone https://github.com/zotanika/HiveMinded.git
cd HiveMinded

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
git clone https://github.com/zotanika/HiveMinded.git
cd HiveMinded

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
git clone https://github.com/zotanika/HiveMinded.git
cd HiveMinded

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
New member installs HiveMinded:
  git clone https://github.com/zotanika/HiveMinded.git
  cd HiveMinded
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
