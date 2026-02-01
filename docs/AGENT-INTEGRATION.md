# Agent Integration Guide

## Overview

Rune is designed to work with **any AI agent** that can:
1. Load skills/tools
2. Connect to MCP (Model Context Protocol) servers
3. Execute structured workflows

This guide shows how to integrate Rune with different agent types.

## Prerequisites

Before integrating any agent, you must:

1. **Sign up for enVector Cloud** at [https://envector.io](https://envector.io)
   - enVector Cloud is the FHE-encrypted vector database (required)
   - Obtain your `org-id` and `api-key` from the dashboard

2. **Deploy a Rune Vault** (see [Team Setup Guide](TEAM-SETUP.md))
   - One Vault per team handles FHE encryption keys

## Supported Agents

### 1. Claude (Anthropic)

**Claude Desktop** and **Claude Code** (VS Code extension) both support MCP natively.

#### Installation

```bash
# Install Rune skills
./install.sh --agent claude

# Skills installed to: ~/.claude/skills/envector
```

#### Configuration

Edit `~/.claude/config.json`:

```json
{
  "mcpServers": {
    "vault": {
      "command": "python",
      "args": ["path/to/rune/mcp/vault/vault_mcp.py"],
      "env": {
        "VAULT_URL": "https://vault-your-team.oci.envector.io",
        "VAULT_TOKEN": "evt_xxx"
      }
    }
  },
  "skills": {
    "envector": {
      "enabled": true
    }
  }
}
```

#### Usage

```
User: Search enVector for decisions about database choice

Claude: [Uses envector skill to search]
        Found: "In Q2 2022, team chose Postgres for JSON support..."
```

### 2. Gemini (Google)

Gemini supports custom extensions through Google AI Studio.

#### Installation

```bash
# Install Rune skills
./install.sh --agent gemini

# Skills installed to: ~/.gemini/skills/envector
```

#### Configuration

Create `~/.gemini/extensions.yaml`:

```yaml
extensions:
  - name: envector
    type: mcp
    servers:
      vault:
        url: https://vault-your-team.oci.envector.io
        token: evt_xxx
    tools:
      - search_context
      - capture_decision
```

#### Usage

```
User: Why did we choose microservices?

Gemini: [Queries envector via MCP]
        "Decision made in Q3 2022: Expected growth to 200 people..."
```

### 3. GitHub Codex (OpenAI)

Codex in GitHub Copilot can be extended with custom skills.

#### Installation

```bash
# Install Rune skills
./install.sh --agent codex

# Skills installed to: ~/.codex/skills/envector
```

#### Configuration

Edit `.copilot/config.json`:

```json
{
  "extensions": {
    "envector": {
      "enabled": true,
      "mcp": {
        "vault": {
          "url": "https://vault-your-team.oci.envector.io",
          "token": "evt_xxx"
        }
      }
    }
  }
}
```

#### Usage

```python
# In code comment
# Why did we choose this architecture?

# Codex uses envector to find context:
"""
Architecture Decision (Q2 2023):
Chose microservices for:
- Independent team deployment
- Better scaling characteristics
Trade-off: Higher operational complexity
"""
```

### 4. Custom Agents

For custom agents, implement MCP client interface.

#### Installation

```bash
# Install to custom location
./install.sh --agent custom --install-dir /path/to/agent/skills
```

#### Python Implementation

```python
from rune import ContextMemory, MCPClient

class CustomAgent:
    def __init__(self, vault_url, vault_token):
        # Initialize MCP client
        self.vault = MCPClient(vault_url, vault_token)
        
        # Initialize context memory
        self.memory = ContextMemory(
            vault_client=self.vault,
            cloud_url="https://api.envector.io"
        )
    
    def capture_decision(self, text, metadata):
        """Capture organizational decision"""
        result = self.memory.capture(
            content=text,
            source=metadata.get("source"),
            author=metadata.get("author"),
            timestamp=metadata.get("timestamp")
        )
        return result
    
    def search_context(self, query):
        """Search organizational memory"""
        results = self.memory.search(
            query=query,
            limit=10,
            mode="accurate"  # or "fast", "exact"
        )
        return self.synthesize(results)
    
    def synthesize(self, results):
        """Synthesize context from search results"""
        # Combine multiple results into coherent answer
        context = []
        for r in results:
            context.append({
                "decision": r.content,
                "when": r.timestamp,
                "who": r.author,
                "source": r.source,
                "relevance": r.score
            })
        return context

# Usage
agent = CustomAgent(
    vault_url="https://vault-your-team.oci.envector.io",
    vault_token="evt_xxx"
)

# Capture
agent.capture_decision(
    "We chose Postgres for JSON support",
    {"source": "slack", "author": "alice"}
)

# Search
results = agent.search_context("Why did we choose Postgres?")
```

#### JavaScript/TypeScript Implementation

```typescript
import { ContextMemory, MCPClient } from 'rune';

class CustomAgent {
  private memory: ContextMemory;
  
  constructor(vaultUrl: string, vaultToken: string) {
    const vault = new MCPClient(vaultUrl, vaultToken);
    this.memory = new ContextMemory({
      vaultClient: vault,
      cloudUrl: 'https://api.envector.io'
    });
  }
  
  async captureDecision(text: string, metadata: any) {
    return await this.memory.capture({
      content: text,
      source: metadata.source,
      author: metadata.author,
      timestamp: metadata.timestamp
    });
  }
  
  async searchContext(query: string) {
    const results = await this.memory.search({
      query: query,
      limit: 10,
      mode: 'accurate'
    });
    return this.synthesize(results);
  }
  
  private synthesize(results: any[]) {
    return results.map(r => ({
      decision: r.content,
      when: r.timestamp,
      who: r.author,
      source: r.source,
      relevance: r.score
    }));
  }
}

// Usage
const agent = new CustomAgent(
  'https://vault-your-team.oci.envector.io',
  'evt_xxx'
);

await agent.captureDecision(
  'We chose Postgres for JSON support',
  { source: 'slack', author: 'alice' }
);

const results = await agent.searchContext('Why did we choose Postgres?');
```

## MCP Protocol Details

### MCP Server Interface

Rune requires one MCP server: **Vault MCP**

#### Vault MCP Endpoints

```
POST /encrypt
  Request:
    {
      "vector": [0.1, 0.5, 0.3, ...],
      "metadata": {"source": "slack", "author": "alice"}
    }
  Response:
    {
      "encrypted_vector": [...],
      "vector_id": "vec_123"
    }

POST /decrypt
  Request:
    {
      "encrypted_result": {...},
      "vector_ids": ["vec_123", "vec_456"]
    }
  Response:
    {
      "results": [
        {"vector_id": "vec_123", "score": 0.95, "metadata": {...}},
        {"vector_id": "vec_456", "score": 0.87, "metadata": {...}}
      ]
    }

GET /health
  Response:
    {"status": "healthy", "version": "0.1.0"}
```

### MCP Client Implementation

```python
import requests

class MCPClient:
    def __init__(self, vault_url, vault_token):
        self.vault_url = vault_url
        self.token = vault_token
        self.session = requests.Session()
        self.session.headers.update({
            'Authorization': f'Bearer {vault_token}',
            'Content-Type': 'application/json'
        })
    
    def encrypt_vector(self, vector, metadata=None):
        """Encrypt vector using team FHE keys"""
        response = self.session.post(
            f'{self.vault_url}/encrypt',
            json={'vector': vector, 'metadata': metadata}
        )
        response.raise_for_status()
        return response.json()
    
    def decrypt_results(self, encrypted_results, vector_ids):
        """Decrypt search results"""
        response = self.session.post(
            f'{self.vault_url}/decrypt',
            json={
                'encrypted_result': encrypted_results,
                'vector_ids': vector_ids
            }
        )
        response.raise_for_status()
        return response.json()
    
    def health_check(self):
        """Check Vault health"""
        response = self.session.get(f'{self.vault_url}/health')
        response.raise_for_status()
        return response.json()
```

## Agent Workflow Patterns

### Pattern 1: Scribe (Capture)

```python
class Scribe:
    def __init__(self, memory):
        self.memory = memory
        self.sources = []  # Slack, Notion, GitHub, etc.
    
    async def watch(self):
        """Continuously monitor sources"""
        while True:
            for source in self.sources:
                # Get new content
                content = await source.fetch_new()
                
                # Detect significant decisions
                if self.is_significant(content):
                    # Capture to memory
                    await self.memory.capture(
                        content=content.text,
                        source=source.name,
                        metadata=content.metadata
                    )
            
            # Wait before next check
            await asyncio.sleep(60)
    
    def is_significant(self, content):
        """Detect if content contains significant decision"""
        patterns = [
            r"we (decided|chose|selected)",
            r"decision:.*",
            r"going with.*because",
            r"trade-off:.*"
        ]
        return any(re.search(p, content.text, re.I) for p in patterns)
```

### Pattern 2: Retriever (Search)

```python
class Retriever:
    def __init__(self, memory):
        self.memory = memory
    
    async def answer_query(self, user_query):
        """Answer 'why' questions with context"""
        
        # Parse intent
        intent = self.parse_intent(user_query)
        
        # Generate search queries
        search_queries = self.expand_query(intent)
        
        # Search memory
        all_results = []
        for q in search_queries:
            results = await self.memory.search(q, limit=5)
            all_results.extend(results)
        
        # Deduplicate and rank
        ranked = self.rank_results(all_results)
        
        # Synthesize answer
        answer = self.synthesize(user_query, ranked[:10])
        
        return answer
    
    def parse_intent(self, query):
        """Understand what user is asking"""
        if "why did we" in query.lower():
            return "decision_rationale"
        elif "when did we" in query.lower():
            return "decision_timeline"
        elif "who decided" in query.lower():
            return "decision_maker"
        else:
            return "general_context"
    
    def synthesize(self, query, results):
        """Combine results into coherent answer"""
        context_parts = []
        
        for r in results:
            context_parts.append(f"""
Decision: {r.content}
When: {r.timestamp}
Who: {r.author}
Source: {r.source}
Relevance: {r.score:.2%}
            """)
        
        # Use LLM to synthesize
        return self.llm_synthesize(query, context_parts)
```

### Pattern 3: Collaborative Agent (Team)

```python
class CollaborativeAgent:
    def __init__(self, memory, agent_id):
        self.memory = memory
        self.agent_id = agent_id
    
    async def work_on_task(self, task):
        """Work on task with team context"""
        
        # Check team context first
        context = await self.memory.search(
            f"relevant context for: {task}",
            limit=5
        )
        
        # Use context to inform work
        result = self.execute_task(task, context)
        
        # Capture my contribution
        await self.memory.capture(
            content=f"Agent {self.agent_id} completed: {task}",
            metadata={
                "task": task,
                "result": result,
                "agent": self.agent_id
            }
        )
        
        return result
```

## Testing Your Integration

### 1. Health Check

```bash
# Test Vault connectivity
curl https://vault-your-team.oci.envector.io/health

# Expected: {"status": "healthy", "version": "0.1.0"}
```

### 2. Encrypt/Decrypt Test

```python
from hiveminded import MCPClient

client = MCPClient(
    vault_url="https://vault-your-team.oci.envector.io",
    vault_token="evt_xxx"
)

# Test encryption
vector = [0.1, 0.5, 0.3]
encrypted = client.encrypt_vector(vector, {"test": "data"})
print(f"Encrypted: {encrypted['vector_id']}")

# Test decryption
results = client.decrypt_results(encrypted, [encrypted['vector_id']])
print(f"Decrypted: {results}")
```

### 3. End-to-End Test

```python
from hiveminded import ContextMemory

memory = ContextMemory(
    vault_url="https://vault-your-team.oci.envector.io",
    vault_token="evt_xxx"
)

# Capture test decision
memory.capture(
    content="Test decision: chose option A over option B",
    source="test",
    metadata={"author": "test-agent"}
)

# Search for it
results = memory.search("test decision option")
assert len(results) > 0
print(f"Found: {results[0].content}")
```

## Troubleshooting

### Common Issues

**1. Connection Refused**
```
Error: Connection refused to vault

Solution:
- Check Vault is running: curl https://vault-your-team.oci.envector.io/health
- Check network/firewall settings
- Verify VAULT_URL environment variable
```

**2. Authentication Failed**
```
Error: 401 Unauthorized

Solution:
- Check VAULT_TOKEN is correct
- Token may be expired (contact team admin)
- Verify token format: starts with "evt_"
```

**3. Encryption Failed**
```
Error: Encryption operation failed

Solution:
- Check Vault has FHE keys initialized
- Verify vector dimension matches (768 by default)
- Check Vault logs for detailed error
```

## Best Practices

1. **Connection Pooling**: Reuse MCP client connections
2. **Error Handling**: Implement retries with exponential backoff
3. **Timeout**: Set reasonable timeouts (5-10s for encryption, 30s for search)
4. **Caching**: Cache frequently accessed context locally
5. **Batching**: Batch multiple operations when possible
6. **Monitoring**: Track latency, error rates, and throughput

## Next Steps

- Review [agent specifications](../agents/)
- Try [example workflows](../examples/)
- Read [security model](SECURITY.md)
- Join community discussions
