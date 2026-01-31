# Agents

This directory contains agent specifications and implementation guides.

## Agent Types

### Monitor Agent

**Role:** Context capture

**Purpose:** Continuously monitors team communications and artifacts to identify and capture significant decisions, architectural rationale, and institutional knowledge.

**Specification:** [monitor-agent.md](monitor-agent.md)

**Key Features:**
- Watches multiple sources (Slack, Notion, GitHub, meetings)
- Detects significant decisions (pattern + ML)
- Extracts context and metadata
- Encrypts and stores in organizational memory

### Retriever Agent

**Role:** Context retrieval and synthesis

**Purpose:** Searches organizational memory for relevant decisions, synthesizes context from multiple sources, and provides actionable insights.

**Specification:** [retriever-agent.md](retriever-agent.md)

**Key Features:**
- Understands user intent
- Searches encrypted organizational memory
- Decrypts results securely
- Synthesizes comprehensive answers
- Provides actionable insights

## Agent Integration

### How Agents Work with HiveMinded

```
┌──────────────────────────────────────┐
│         Your AI Agent                │
│      (Claude/Gemini/Codex)           │
├──────────────────────────────────────┤
│  1. Load skills from skills/         │
│  2. Connect to MCP servers           │
│  3. Execute agent behaviors          │
│     - Monitor: Capture context       │
│     - Retriever: Search & synthesize │
└──────────────────────────────────────┘
```

### Agent Workflow

**Monitor Agent Workflow:**

```
1. Watch configured sources
   ├─ Slack channels
   ├─ Notion pages
   ├─ GitHub PRs/Issues
   └─ Meeting transcripts

2. Detect significant events
   ├─ Pattern matching
   ├─ ML classification
   └─ Human review (optional)

3. Extract context
   ├─ Decision content
   ├─ Participants
   ├─ Timestamp
   └─ Related artifacts

4. Encrypt and store
   ├─ Generate embedding
   ├─ Encrypt via Vault MCP
   └─ Store in enVector Cloud
```

**Retriever Agent Workflow:**

```
1. Parse user query
   └─ Understand intent

2. Generate search queries
   └─ Expand with synonyms

3. Search memory
   ├─ Encrypt query (Vault MCP)
   ├─ FHE search (enVector Cloud)
   └─ Decrypt results (Vault MCP)

4. Synthesize answer
   ├─ Rank results by relevance
   ├─ Combine multiple sources
   └─ Provide actionable insights
```

## Implementing Agent Behaviors

### Python Example

```python
from hiveminded import ContextMemory, MonitorAgent, RetrieverAgent

# Initialize memory
memory = ContextMemory(
    vault_url="https://vault-your-team.oci.envector.io",
    vault_token="evt_xxx"
)

# Monitor agent
monitor = MonitorAgent(memory)
monitor.watch_source("slack", channels=["#engineering"])
monitor.watch_source("github", repos=["owner/repo"])
monitor.start()

# Retriever agent
retriever = RetrieverAgent(memory)
answer = retriever.answer_query("Why did we choose Postgres?")
print(answer)
```

### JavaScript/TypeScript Example

```typescript
import { ContextMemory, MonitorAgent, RetrieverAgent } from 'hiveminded';

// Initialize memory
const memory = new ContextMemory({
  vaultUrl: 'https://vault-your-team.oci.envector.io',
  vaultToken: 'evt_xxx'
});

// Monitor agent
const monitor = new MonitorAgent(memory);
monitor.watchSource('slack', { channels: ['#engineering'] });
monitor.watchSource('github', { repos: ['owner/repo'] });
await monitor.start();

// Retriever agent
const retriever = new RetrieverAgent(memory);
const answer = await retriever.answerQuery('Why did we choose Postgres?');
console.log(answer);
```

## Custom Agent Behaviors

You can implement custom agent behaviors by extending base classes:

```python
from hiveminded import BaseAgent

class CustomAgent(BaseAgent):
    def __init__(self, memory):
        super().__init__(memory)
    
    async def custom_behavior(self):
        # Your custom logic
        results = await self.memory.search("custom query")
        return self.process(results)
    
    def process(self, results):
        # Custom processing logic
        return {"processed": results}

# Use custom agent
agent = CustomAgent(memory)
result = await agent.custom_behavior()
```

## Agent Configuration

### Environment Variables

```bash
# Vault configuration
export VAULT_URL="https://vault-your-team.oci.envector.io"
export VAULT_TOKEN="evt_xxx"

# Cloud configuration (optional)
export CLOUD_URL="https://api.envector.io"

# Agent-specific settings
export AGENT_LOG_LEVEL="INFO"
export AGENT_CAPTURE_THRESHOLD=0.8  # ML confidence threshold
export AGENT_SEARCH_LIMIT=10        # Max results per search
```

### Configuration Files

**~/.claude/agent-config.yaml** (example for Claude):

```yaml
monitor:
  sources:
    slack:
      enabled: true
      channels: ["#engineering", "#product"]
      patterns:
        - "decision:"
        - "we chose"
        - "architecture:"
    github:
      enabled: true
      repos: ["owner/repo"]
      events: ["pull_request", "issue_comment"]
  
  capture:
    auto_capture: true
    confidence_threshold: 0.8
    human_review: false

retriever:
  search:
    mode: "accurate"  # fast, accurate, exact
    limit: 10
    timeout_ms: 5000
  
  synthesis:
    max_sources: 5
    include_metadata: true
```

## Testing Agents

### Unit Tests

```python
# tests/test_monitor_agent.py
import pytest
from hiveminded import MonitorAgent, ContextMemory

@pytest.fixture
def mock_memory():
    return MockContextMemory()

def test_monitor_detects_decision(mock_memory):
    monitor = MonitorAgent(mock_memory)
    
    text = "Decision: We chose Postgres for JSON support"
    is_significant = monitor.is_significant(text)
    
    assert is_significant == True

def test_monitor_captures_context(mock_memory):
    monitor = MonitorAgent(mock_memory)
    
    result = monitor.capture(
        text="We chose Postgres",
        source="slack",
        metadata={"channel": "#eng"}
    )
    
    assert result.success == True
    assert mock_memory.captured_count == 1
```

### Integration Tests

```python
# tests/test_integration.py
import pytest
from hiveminded import ContextMemory, MonitorAgent, RetrieverAgent

@pytest.mark.integration
async def test_capture_and_retrieve():
    # Use test Vault
    memory = ContextMemory(
        vault_url="http://localhost:50080",
        vault_token="test_token"
    )
    
    # Capture
    monitor = MonitorAgent(memory)
    await monitor.capture(
        text="Test decision: chose option A",
        source="test"
    )
    
    # Retrieve
    retriever = RetrieverAgent(memory)
    results = await retriever.search("test decision")
    
    assert len(results) > 0
    assert "option A" in results[0].content
```

## Best Practices

1. **Error Handling**: Always handle MCP connection failures
2. **Rate Limiting**: Respect source API rate limits
3. **Logging**: Log all significant events for debugging
4. **Testing**: Write tests for both capture and retrieval
5. **Configuration**: Make agents configurable via environment
6. **Documentation**: Document custom behaviors clearly

## Troubleshooting

### Agent not capturing context

**Check:**
1. Sources configured correctly?
2. Patterns matching content?
3. Confidence threshold too high?
4. Vault connection working?

**Debug:**
```python
agent.set_log_level("DEBUG")
agent.test_source_connection("slack")
agent.test_pattern_matching("sample text")
```

### Agent not finding context

**Check:**
1. Context actually captured?
2. Query embedding correct?
3. Search mode appropriate?
4. Vault decryption working?

**Debug:**
```python
retriever.set_log_level("DEBUG")
results = retriever.search("query", debug=True)
print(results.debug_info)
```

## Next Steps

- Read agent specifications: [monitor-agent.md](monitor-agent.md), [retriever-agent.md](retriever-agent.md)
- Try example workflows: [../examples/](../examples/)
- Integrate with your agent: [../docs/AGENT-INTEGRATION.md](../docs/AGENT-INTEGRATION.md)
- Join community discussions
