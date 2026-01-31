# HiveMinded Architecture

## System Overview

HiveMinded is an **agent-agnostic organizational context memory system** built on three core principles:

1. **Capture**: Automatically identify and capture significant decisions
2. **Encrypt**: Store as FHE-encrypted vectors (searchable but cryptographically private)
3. **Retrieve**: Search and synthesize context on demand

## Components

### 1. Skills Layer (Agent Interface)

**Purpose**: Provides standardized capabilities to any AI agent

```
┌─────────────────────────────────────────┐
│         Agent (Claude/Gemini/Codex)     │
├─────────────────────────────────────────┤
│  Skills:                                │
│  - envector: Organizational memory      │
│  - (custom skills)                      │
└─────────────────────────────────────────┘
```

**Key Features:**
- Agent-agnostic interface (MCP protocol)
- Skill discovery and loading
- Configuration management
- Error handling and retries

### 2. Agent Layer (Behavior Specification)

**Purpose**: Defines agent behaviors and workflows

```
┌──────────────────┐  ┌──────────────────┐
│  Monitor Agent   │  │ Retriever Agent  │
├──────────────────┤  ├──────────────────┤
│ - Watch sources  │  │ - Parse queries  │
│ - Detect context │  │ - Search memory  │
│ - Capture        │  │ - Synthesize     │
│ - Encrypt        │  │ - Respond        │
└──────────────────┘  └──────────────────┘
```

**Monitor Agent Workflow:**
1. Watch configured sources (Slack, Notion, GitHub, etc.)
2. Detect significant decisions (pattern + ML)
3. Extract context and metadata
4. Encrypt as vectors (via Vault)
5. Store in enVector Cloud

**Retriever Agent Workflow:**
1. Parse user query (understand intent)
2. Generate search embeddings
3. Search encrypted memory (FHE)
4. Decrypt results (via Vault)
5. Synthesize comprehensive answer

### 3. MCP Layer (Protocol Interface)

**Purpose**: Standardized communication between agents and services

```
┌──────────────────────────────────────────┐
│            MCP Protocol                  │
├──────────────────────────────────────────┤
│  Servers:                                │
│  - Vault MCP: Key management             │
│  - enVector MCP: Cloud search            │
│  - (custom MCP servers)                  │
└──────────────────────────────────────────┘
```

**Vault MCP Server:**
- Manages FHE keys (never exposed to agents)
- Encrypts vectors before storage
- Decrypts search results
- Runs in isolated environment (TEE/secure network)

**enVector MCP Client:**
- Connects to enVector Cloud
- Submits encrypted queries
- Retrieves encrypted results
- Never sees plaintext data

### 4. Storage Layer (Data Management)

**Purpose**: Store and search encrypted vectors

```
┌──────────────────────────────────────────┐
│         enVector Cloud (Optional)        │
├──────────────────────────────────────────┤
│  - FHE-encrypted vector store            │
│  - IVF-GAS search (data-oblivious)       │
│  - Team isolation                        │
│  - Replication and backup                │
└──────────────────────────────────────────┘
```

**Key Features:**
- All data encrypted (FHE)
- Cloud never sees plaintext
- Semantic search on encrypted data
- Scalable (millions of vectors)

## Data Flow

### Capture Flow

```
Slack Thread
    │
    ▼
Monitor Agent (detects significant decision)
    │
    ▼
Extract Context ("We chose Postgres for JSON support")
    │
    ▼
Generate Embedding ([0.1, 0.5, 0.3, ...])
    │
    ▼
Vault MCP (encrypt with team FHE keys)
    │
    ▼
Encrypted Vector ([enc(0.1), enc(0.5), enc(0.3), ...])
    │
    ▼
enVector Cloud (store encrypted)
```

### Retrieval Flow

```
User Query ("Why did we choose Postgres?")
    │
    ▼
Retriever Agent (parse intent)
    │
    ▼
Generate Query Embedding ([0.2, 0.4, 0.4, ...])
    │
    ▼
Vault MCP (encrypt query)
    │
    ▼
Encrypted Query ([enc(0.2), enc(0.4), enc(0.4), ...])
    │
    ▼
enVector Cloud (FHE search, returns encrypted results)
    │
    ▼
Vault MCP (decrypt results)
    │
    ▼
Plaintext Results ("Postgres chosen for JSON support...")
    │
    ▼
Retriever Agent (synthesize answer)
    │
    ▼
User ("In Q2 2022, team chose Postgres because...")
```

## Security Model

### Zero-Trust Architecture

**Principle**: No component trusts any other component

```
┌────────────────────────────────────────┐
│  Threat Model:                         │
│  - Cloud provider compromised          │
│  - Agent compromised (prompt injection)│
│  - Network eavesdropping               │
│  - Insider threat                      │
└────────────────────────────────────────┘
         │
         ▼
┌────────────────────────────────────────┐
│  Defense: FHE + Isolation              │
│  - Data encrypted at source            │
│  - Keys isolated in Vault              │
│  - Cloud cannot decrypt                │
│  - Agents cannot access keys           │
└────────────────────────────────────────┘
```

### Key Management

**Keys Never Leave Vault:**

```
┌─────────────────────────────────────────┐
│            Team Vault                   │
│  ┌────────────────────────────────┐    │
│  │  FHE Keys (encrypted at rest)  │    │
│  │  - Public key: Encrypt vectors │    │
│  │  - Secret key: Decrypt results │    │
│  │  - Eval key: FHE operations    │    │
│  └────────────────────────────────┘    │
│                                         │
│  Operations:                            │
│  - encrypt(vector) → enc_vector         │
│  - decrypt(enc_result) → result         │
│  - NEVER: export keys                   │
└─────────────────────────────────────────┘
```

**Access Control:**
- Vault authenticates agents (JWT tokens)
- Rate limiting per agent
- Audit logging (who accessed what, when)
- Key rotation support

### Data Isolation

**Team Isolation:**

```
Team A                    Team B
   │                         │
   ▼                         ▼
Vault A (keys A)       Vault B (keys B)
   │                         │
   ▼                         ▼
Cloud (encrypted A)    Cloud (encrypted B)
   │                         │
   └─────────┬───────────────┘
             │
             ▼
   Cross-team search: Impossible
   (Different keys = different ciphertexts)
```

## Deployment Models

### 1. Hybrid (Recommended)

```
┌─────────────────────────────────────────┐
│       Your Infrastructure               │
│  ┌────────────────────────────────┐    │
│  │  Team Vault (your keys)        │    │
│  │  - OCI Vault / AWS KMS / GCP   │    │
│  │  - OR self-hosted              │    │
│  └────────────────────────────────┘    │
└─────────────────────────────────────────┘
              │ HTTPS + JWT
              ▼
┌─────────────────────────────────────────┐
│       enVector Cloud (SaaS)             │
│  - Encrypted vectors only               │
│  - FHE search                           │
│  - Scalability and reliability          │
└─────────────────────────────────────────┘
```

**Benefits:**
- You control keys (security)
- We manage scale (reliability)
- Team shares context automatically
- Simple setup (one Vault per team)

### 2. Fully On-Premise

```
┌─────────────────────────────────────────┐
│       Your Datacenter                   │
│  ┌────────────────────────────────┐    │
│  │  Team Vault (your keys)        │    │
│  └────────────────────────────────┘    │
│  ┌────────────────────────────────┐    │
│  │  enVector Service (your infra) │    │
│  └────────────────────────────────┘    │
└─────────────────────────────────────────┘
```

**Benefits:**
- Full control (security + data sovereignty)
- Air-gapped if needed
- Regulatory compliance (ITAR, classified data)

**Trade-offs:**
- You manage everything
- Higher operational cost
- Enterprise pricing ($500K+/year)

## Scalability

### Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Search latency | < 100ms | P95, encrypted search |
| Capture latency | < 500ms | Background, async |
| Throughput | > 100 QPS | Per team |
| Storage | Unlimited | Pay-as-you-grow |

### Scaling Strategy

**Horizontal Scaling:**

```
Load Balancer
    │
    ├── Vault Instance 1 (keys 1)
    ├── Vault Instance 2 (keys 2)
    └── Vault Instance N (keys N)
    
    Each instance: One team or team shard
```

**Data Sharding:**

```
Team Data (1M vectors)
    │
    ├── Recent (100K) → FLAT search (fast)
    ├── Archive (900K) → IVF-GAS (accurate)
    └── Cold (older) → Compressed storage
```

## Extensibility

### Adding New Agents

1. Implement MCP client
2. Load skills from `skills/` directory
3. Connect to Vault MCP
4. Use standard APIs

See [AGENT-INTEGRATION.md](AGENT-INTEGRATION.md)

### Adding New Skills

1. Create skill directory
2. Write SKILL.md (documentation)
3. Define tools (if needed)
4. Implement logic
5. Test with multiple agents

See [skills/README.md](../skills/README.md)

### Adding New Storage Backends

1. Implement storage interface
2. Support encryption (FHE)
3. Implement search (IVF-GAS)
4. Add deployment scripts

## Monitoring and Observability

### Metrics

**Vault Metrics:**
- Encryption operations/sec
- Decryption operations/sec
- Key access frequency
- Error rates

**Search Metrics:**
- Query latency (P50, P95, P99)
- Throughput (QPS)
- Recall (quality)
- Cache hit rate

**Agent Metrics:**
- Capture rate (decisions/day)
- Query rate (queries/day)
- Success rate (%)
- User satisfaction

### Logging

**Structured Logs:**
```json
{
  "timestamp": "2026-01-31T12:00:00Z",
  "level": "INFO",
  "component": "vault",
  "operation": "decrypt",
  "team": "your-team",
  "user": "alice",
  "latency_ms": 15,
  "status": "success"
}
```

**Audit Trail:**
- Who accessed what context
- When and from where
- Query patterns
- Compliance reporting

## Future Enhancements

### Planned (v0.2.0+)

- [ ] Multi-modal context (images, audio, video)
- [ ] Real-time collaboration
- [ ] Advanced ML capture (better precision)
- [ ] Web UI for context browsing
- [ ] API gateway for non-MCP agents
- [ ] Cross-team context sharing (with permission)

### Research (v1.0.0+)

- [ ] Differential privacy (DP + FHE)
- [ ] Federated learning (shared models)
- [ ] Semantic compression (reduce storage)
- [ ] Query understanding (NLU improvements)
