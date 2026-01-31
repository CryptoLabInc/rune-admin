# Team Collaboration Example

This example shows how 3 developers collaborate on a confidential project using HiveMinded with different AI agents.

## Scenario

**Project:** Porting [fhenomenon](https://github.com/zotanika/fhenomenon) FHE library to game engine  
**Team:** Alice (Claude), Bob (Gemini), Carol (Codex)  
**Goal:** Share AI agent context seamlessly without manual sync

## Setup

### Prerequisites

1. HiveMinded installed for each team member
2. Team Vault deployed and credentials shared
3. Each member using their preferred agent

### Team Vault

```bash
# Admin deployed once
./scripts/deploy-vault.sh \
  --provider oci \
  --team-name fhenomenon-game

# Output (shared with team):
VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
VAULT_TOKEN="evt_fhen_game_abc123xyz"
```

### Alice's Setup (Claude)

```bash
cd HiveMinded
./install.sh --agent claude

export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

./scripts/configure-agent.sh --agent claude
```

### Bob's Setup (Gemini)

```bash
cd HiveMinded
./install.sh --agent gemini

export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

./scripts/configure-agent.sh --agent gemini
```

### Carol's Setup (Codex)

```bash
cd HiveMinded
./install.sh --agent codex

export VAULT_URL="https://vault-fhenomenon-game.oci.envector.io"
export VAULT_TOKEN="evt_fhen_game_abc123xyz"

./scripts/configure-agent.sh --agent codex
```

## Workflow

### Day 1: Alice Reviews FHE Approach

**Alice's conversation with Claude:**

```
Alice: I'm reviewing the FHE memory optimization in fhenomenon.
       Looking at the ciphertext packing implementation.

Claude: Let me analyze the fhenomenon code...
        
        The ciphertext packing approach:
        - Packs multiple plaintexts into one ciphertext
        - Reduces memory by ~60% (4 GB → 1.6 GB)
        - Adds 15ms encryption overhead
        - Uses SIMD operations for parallel processing
        
        For game engine context:
        - 60fps = 16.67ms frame budget
        - 15ms FHE overhead = 90% of frame budget
        - Need to run in background thread

Alice: Capture this decision: We'll use ciphertext packing for 
       memory efficiency (60% savings). Accept 15ms latency by 
       running FHE operations in background thread, preventing 
       frame drops.

Claude: ✓ Decision captured to team memory
        
        Summary:
        - Component: FHE Memory Optimization
        - Decision: Ciphertext packing in background thread
        - Benefit: 60% memory reduction
        - Cost: 15ms latency (handled async)
        - Rationale: Game needs 60fps (16ms budget)
```

**Behind the scenes:**
```python
# Claude Code captures context
context = {
    "decision": "Use ciphertext packing for memory efficiency",
    "component": "fhe-memory",
    "rationale": "60% memory savings, 15ms latency handled async",
    "author": "alice",
    "timestamp": "2026-01-31T10:00:00Z"
}

# Encrypt and store
memory.capture(
    content=context["decision"] + " - " + context["rationale"],
    metadata=context
)
# → Encrypted with team FHE keys
# → Stored in enVector Cloud
```

### Day 2: Bob Discovers Alice's Context

**Bob's conversation with Gemini:**

```
Bob: I'm implementing memory management for the FHE layer.
     What's our approach for memory optimization?

Gemini: Let me search team context...
        
        Found decision from Alice (yesterday):
        
        "Use ciphertext packing for memory efficiency (60% savings). 
         Accept 15ms latency by running FHE operations in background 
         thread, preventing frame drops."
        
        Implications for your memory manager:
        1. Allocate buffers for packed ciphertexts (1.6GB vs 4GB)
        2. Run FHE operations in dedicated thread pool
        3. Use async queue for game→FHE communication
        4. Budget: 15ms max for FHE operations
        
        Would you like me to draft the memory manager structure?

Bob: Yes, please draft it following Alice's design.

Gemini: [Drafts memory manager code]
        
        class FHEMemoryManager {
            // Packed ciphertext pool (1.6GB budget)
            CiphertextPool pool(1.6 * GB);
            
            // Background thread for FHE operations
            ThreadPool fhe_threads(4);
            
            // Async queue
            AsyncQueue<FHEOperation> queue;
            
            async encrypt(plaintext) {
                // Budget: 15ms max
                return fhe_threads.submit([&]() {
                    return pack_and_encrypt(plaintext);
                });
            }
        }

Bob: Capture: Implemented FHE memory manager following Alice's 
     ciphertext packing design. Uses 1.6GB pool and 4-thread 
     background executor. Async API prevents frame drops.

Gemini: ✓ Decision captured to team memory
```

**Behind the scenes:**
```python
# Bob's Gemini queries enVector
query = "memory optimization FHE approach"

# Same team Vault → same keys → finds Alice's context
results = memory.search(query)
# Returns Alice's encrypted context, decrypted by Vault

# Bob's contribution also captured
memory.capture(
    content="Implemented FHE memory manager with async API",
    metadata={
        "component": "fhe-memory",
        "author": "bob",
        "related_to": "alice_ciphertext_packing_decision"
    }
)
```

### Day 3: Carol Integrates Shader Pipeline

**Carol's conversation with Codex:**

```
Carol: // Working on shader integration with FHE layer
       // Need to understand the memory architecture

Codex: [Searches team context]
       
       /*
        * Team Context: FHE Memory Architecture
        * 
        * Day 1 (Alice): Ciphertext packing approach
        * - 60% memory savings (4GB → 1.6GB)
        * - 15ms encryption overhead
        * - Background thread to prevent frame drops
        * 
        * Day 2 (Bob): Memory manager implementation
        * - 1.6GB packed ciphertext pool
        * - 4-thread background executor
        * - Async API: encrypt() returns future
        * 
        * For shader integration:
        * 1. Call FHEMemoryManager.encrypt() (async)
        * 2. Continue shader work while encrypting
        * 3. Sync when needed (before upload to server)
        */

Carol: // Perfect! Draft shader integration code

Codex: class ShaderPipeline {
           FHEMemoryManager& fhe;
           
           void process_frame(Frame& frame) {
               // 1. Render frame (10ms)
               render(frame);
               
               // 2. Start FHE encryption (async, 15ms)
               auto fhe_future = fhe.encrypt(frame.sensitive_data);
               
               // 3. Continue with post-processing (5ms)
               post_process(frame);
               
               // 4. Wait for encryption if needed
               auto encrypted = fhe_future.get();
               
               // Total: 10ms + max(15ms, 5ms) = 25ms
               // But we're async so: 10ms + 5ms = 15ms render time
               // FHE runs in parallel: OK!
           }
       };

Carol: Capture: Integrated shader pipeline with FHE memory manager.
       Render and FHE encryption run in parallel. Render budget: 15ms,
       FHE runs async so no frame impact.

Codex: ✓ Decision captured to team memory
```

**Behind the scenes:**
```python
# Carol's Codex sees full team context
query = "FHE memory architecture shader integration"

results = memory.search(query)
# Returns:
# 1. Alice's ciphertext packing decision
# 2. Bob's memory manager implementation
# 3. Relevant design rationale

# Carol's integration also captured
memory.capture(
    content="Shader pipeline integrated with async FHE encryption",
    metadata={
        "component": "shader-fhe-integration",
        "author": "carol",
        "builds_on": ["alice_packing", "bob_memory_manager"]
    }
)
```

## Key Benefits

### 1. Seamless Context Sharing

```
✓ Alice captures decision → Bob sees it automatically
✓ Bob implements → Carol sees both Alice and Bob's work
✓ No manual sync, no repeated questions
✓ Different agents (Claude/Gemini/Codex) all work together
```

### 2. Decision Continuity

```
Day 1: "Why 60% memory savings?"
Day 2: "Oh, Alice already decided - ciphertext packing"
Day 3: "Let me build on Bob's memory manager"

vs. Without HiveMinded:

Day 1: Alice decides
Day 2: Bob asks Alice (she's offline)
Day 3: Carol repeats Bob's questions
```

### 3. Onboarding New Members

```
New developer joins Day 4:

New Dev: "What's our FHE approach?"

Agent: [Searches team memory]
       Full context from Alice, Bob, Carol:
       - Architecture decisions
       - Implementation details
       - Rationale and trade-offs
       - Code examples

New Dev: "Perfect, I understand everything!"

Time to productivity: 10 minutes vs. 2 days
```

## Metrics

**Before HiveMinded:**
- Context sharing: Manual (Slack, docs, meetings)
- Repeated questions: 5-10 per day
- Onboarding time: 2-3 days per person
- Context loss: When people leave

**With HiveMinded:**
- Context sharing: Automatic (via FHE encryption)
- Repeated questions: 0 (agents search team memory)
- Onboarding time: 10-30 minutes
- Context loss: Never (all captured and searchable)

## Security

**Confidential Project Protection:**

```
✓ All context encrypted with FHE
✓ enVector Cloud never sees plaintext
✓ Team Vault isolated (OCI/AWS/GCP managed)
✓ Keys never leave Vault
✓ Even compromised cloud can't decrypt

Perfect for:
- Confidential game engine work
- Proprietary algorithms
- Business secrets
- Regulated industries
```

## Next Steps

1. Try this workflow with your team
2. Customize capture rules for your domain
3. Add more team members
4. Scale to multiple projects

See [Team Setup Guide](../../docs/TEAM-SETUP.md) for details.
