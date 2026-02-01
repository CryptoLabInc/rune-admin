# Rune Architecture

## System Overview

Rune is an **agent-agnostic organizational context memory system** built on three core principles:

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

**Scribe Workflow:**
1. Watch configured sources (Slack, Notion, GitHub, etc.)
2. Detect significant decisions (pattern + ML)
3. Extract context and metadata
4. Encrypt as vectors (via envector-mcp-server using public keys from Vault)
5. Store in enVector Cloud

**Retriever Workflow:**
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
│  - Rune Vault: Key management             │
│  - enVector MCP: Cloud search            │
│  - (custom MCP servers)                  │
└──────────────────────────────────────────┘
```

**Rune Vault MCP Server:**
- Manages FHE keys (SecKey never exposed)
- Distributes public keys (EncKey, EvalKey) to envector-mcp-server
- Decrypts search results (only component with SecKey)
- Runs in isolated environment (TEE/secure network)

**envector-mcp-server:**
- Receives public keys from Vault at startup
- Encrypts vectors and queries using EncKey
- Connects to enVector Cloud
- Submits encrypted queries, retrieves encrypted results
- Scalable (multiple instances can share same keys)

### 4. Storage Layer (Data Management)

**Purpose**: Store and search encrypted vectors

> **Note**: enVector Cloud ([https://envector.io](https://envector.io)) is required. Sign up to obtain your API credentials before proceeding.

```
┌──────────────────────────────────────────┐
│   enVector Cloud (https://envector.io)   │
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
Scribe (detects significant decision)
    │
    ▼
Extract Context ("We chose Postgres for JSON support")
    │
    ▼
Generate Embedding ([0.1, 0.5, 0.3, ...])
    │
    ▼
envector-mcp-server (encrypt with EncKey from Vault)
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
Retriever (parse intent)
    │
    ▼
Generate Query Embedding ([0.2, 0.4, 0.4, ...])
    │
    ▼
envector-mcp-server (encrypt query with EncKey)
    │
    ▼
Encrypted Query ([enc(0.2), enc(0.4), enc(0.4), ...])
    │
    ▼
enVector Cloud (FHE search, returns encrypted results)
    │
    ▼
Vault MCP (decrypt results with SecKey)
    │
    ▼
Plaintext Results ("Postgres chosen for JSON support...")
    │
    ▼
Retriever (synthesize answer)
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

**Secret Key Never Leaves Vault:**

```
┌─────────────────────────────────────────┐
│            Rune Vault                   │
│  ┌────────────────────────────────┐    │
│  │  FHE Keys (encrypted at rest)  │    │
│  │  - Secret key: Decrypt results │ ← NEVER exposed    │
│  │  - Public key: Distributed     │    │
│  │  - Eval key: Distributed       │    │
│  └────────────────────────────────┘    │
│                                         │
│  Operations:                            │
│  - get_public_key() → EncKey, EvalKey   │
│  - decrypt(enc_result) → result         │
│  - NEVER: export SecKey                 │
└─────────────────────────────────────────┘

┌─────────────────────────────────────────┐
│       envector-mcp-server(s)            │
│  ┌────────────────────────────────┐    │
│  │  Public Keys (from Vault)      │    │
│  │  - EncKey: Encrypt vectors     │    │
│  │  - EvalKey: FHE operations     │    │
│  └────────────────────────────────┘    │
│                                         │
│  Operations:                            │
│  - encrypt(vector) → enc_vector         │
│  - search(enc_query) → enc_results      │
│  - Scalable: Multiple instances OK      │
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
│  │  Rune Vault (your keys)        │    │
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
│  │  Rune Vault (your keys)        │    │
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
                    ┌──────────────────┐
                    │      Rune Vault      │  ← Single instance per team
                    │   (SecKey only)  │     (decryption is lightweight)
                    └────────┬─────────┘
                             │ EncKey, EvalKey
                             ▼
Load Balancer ──────────────────────────────────────
    │
    ├── envector-mcp-server 1 (encryption + search)
    ├── envector-mcp-server 2 (encryption + search)
    └── envector-mcp-server N (encryption + search)

    Each instance: Same public keys, scales horizontally
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
- [ ] API gateway for non-MCP agents
- [ ] Cross-team context sharing (with permission)

### Research (v1.0.0+)

- [ ] Differential privacy (DP + FHE)
- [ ] Federated learning (shared models)
- [ ] Semantic compression (reduce storage)
- [ ] Query understanding (NLU improvements)
