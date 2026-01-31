# MCP Servers

This directory contains MCP (Model Context Protocol) server implementations for HiveMinded.

## Available Servers

### Vault MCP

**Purpose:** FHE key management and cryptographic operations

**Location:** `vault/`

**Features:**
- Manages FHE encryption keys (team-shared)
- Encrypts vectors before storage
- Decrypts search results
- Runs in isolated environment (TEE/secure network)
- Never exposes keys to agents

**Documentation:** [vault/README.md](vault/README.md)

**Deployment:**
```bash
# Production (managed)
../scripts/deploy-vault.sh --provider oci --team-name your-team

# Development (local)
../scripts/vault-dev.sh
```

### enVector Client MCP (Coming Soon)

**Purpose:** Connect to enVector Cloud for encrypted search

**Location:** `envector-client/`

**Features:**
- Submits encrypted queries to enVector Cloud
- Retrieves encrypted results
- Never sees plaintext data
- Handles connection pooling and retries

## MCP Protocol Overview

MCP (Model Context Protocol) is a standard protocol for AI agents to communicate with external services.

### Protocol Flow

```
┌──────────┐                  ┌──────────┐
│  Agent   │                  │   MCP    │
│          │                  │  Server  │
└──────────┘                  └──────────┘
     │                              │
     │  1. Connect                  │
     ├─────────────────────────────>│
     │                              │
     │  2. List Tools               │
     ├─────────────────────────────>│
     │  <tools available>           │
     │<─────────────────────────────┤
     │                              │
     │  3. Call Tool                │
     │     (encrypt_vector)         │
     ├─────────────────────────────>│
     │  <encrypted result>          │
     │<─────────────────────────────┤
     │                              │
     │  4. Call Tool                │
     │     (decrypt_results)        │
     ├─────────────────────────────>│
     │  <decrypted data>            │
     │<─────────────────────────────┤
```

### Message Format

**Tool Call:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "encrypt_vector",
    "arguments": {
      "vector": [0.1, 0.5, 0.3],
      "metadata": {"source": "slack"}
    }
  }
}
```

**Tool Response:**
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "encrypted_vector": [...],
    "vector_id": "vec_123"
  }
}
```

## Creating Custom MCP Servers

### 1. Server Structure

```python
from mcp import MCPServer, Tool

class CustomMCPServer(MCPServer):
    def __init__(self, config):
        super().__init__("custom-server", "1.0.0")
        self.config = config
        
        # Register tools
        self.register_tool(
            Tool(
                name="custom_tool",
                description="What this tool does",
                input_schema={
                    "type": "object",
                    "properties": {
                        "param": {"type": "string"}
                    },
                    "required": ["param"]
                },
                handler=self.handle_custom_tool
            )
        )
    
    async def handle_custom_tool(self, param: str):
        """Handle custom tool invocation"""
        result = self.process(param)
        return {"result": result}

# Start server
server = CustomMCPServer(config)
server.run()
```

### 2. Tool Registration

```python
@server.tool(
    name="my_tool",
    description="Tool description",
    input_schema={...}
)
async def my_tool_handler(ctx, arg1, arg2):
    # Implementation
    return {"status": "success"}
```

### 3. Error Handling

```python
async def handle_tool(self, param):
    try:
        result = self.process(param)
        return {"result": result}
    except ValueError as e:
        raise MCPError(
            code="INVALID_PARAMS",
            message=str(e)
        )
    except Exception as e:
        raise MCPError(
            code="INTERNAL_ERROR",
            message="Processing failed"
        )
```

## Server Deployment

### Local Development

```bash
# Start server locally
python mcp/your-server/server.py \
  --port 50080 \
  --log-level DEBUG
```

### Production (Docker)

```dockerfile
FROM python:3.11-slim

WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt

COPY server.py .

EXPOSE 50080

CMD ["python", "server.py", "--port", "50080"]
```

```bash
# Build and run
docker build -t your-mcp-server .
docker run -d -p 50080:50080 your-mcp-server
```

### Production (Kubernetes)

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: mcp-server
  template:
    metadata:
      labels:
        app: mcp-server
    spec:
      containers:
      - name: server
        image: your-mcp-server:latest
        ports:
        - containerPort: 50080
        env:
        - name: LOG_LEVEL
          value: "INFO"
---
apiVersion: v1
kind: Service
metadata:
  name: mcp-server
spec:
  selector:
    app: mcp-server
  ports:
  - port: 50080
    targetPort: 50080
```

## Security Considerations

### 1. Authentication

```python
from mcp import MCPServer, require_auth

class SecureMCPServer(MCPServer):
    @require_auth
    async def handle_tool(self, token, param):
        # Verify token
        if not self.verify_token(token):
            raise MCPError(code="UNAUTHORIZED")
        
        # Process request
        return {"result": ...}
```

### 2. Rate Limiting

```python
from mcp import RateLimiter

limiter = RateLimiter(
    max_requests=100,
    window_seconds=60
)

@limiter.limit
async def handle_tool(self, param):
    # Process request
    pass
```

### 3. Input Validation

```python
from pydantic import BaseModel, validator

class ToolInput(BaseModel):
    param: str
    
    @validator('param')
    def validate_param(cls, v):
        if len(v) > 1000:
            raise ValueError("Param too long")
        return v

async def handle_tool(self, input: ToolInput):
    # Input is validated
    return {"result": ...}
```

## Testing MCP Servers

### Unit Tests

```python
import pytest
from mcp.testing import MCPTestClient

@pytest.fixture
async def client():
    server = CustomMCPServer(config)
    async with MCPTestClient(server) as client:
        yield client

async def test_tool_call(client):
    response = await client.call_tool(
        "custom_tool",
        {"param": "test"}
    )
    assert response["result"] == "expected"
```

### Integration Tests

```python
@pytest.mark.integration
async def test_server_integration():
    # Start server
    server = start_test_server()
    
    # Connect client
    client = MCPClient("http://localhost:50080")
    
    # Test tool
    result = await client.call_tool("custom_tool", {...})
    
    assert result["status"] == "success"
    
    # Cleanup
    server.stop()
```

## Monitoring

### Health Check

```python
@server.health_check
async def check_health():
    return {
        "status": "healthy",
        "version": "1.0.0",
        "uptime": server.uptime(),
        "connections": server.active_connections()
    }
```

### Metrics

```python
from prometheus_client import Counter, Histogram

tool_calls = Counter(
    'mcp_tool_calls_total',
    'Total tool calls',
    ['tool_name', 'status']
)

tool_latency = Histogram(
    'mcp_tool_latency_seconds',
    'Tool call latency',
    ['tool_name']
)

@tool_latency.time()
async def handle_tool(self, param):
    result = self.process(param)
    tool_calls.labels(tool_name='custom_tool', status='success').inc()
    return result
```

## Best Practices

1. **Versioning**: Version your MCP server APIs
2. **Documentation**: Document all tools and parameters
3. **Error Handling**: Return meaningful error messages
4. **Logging**: Log all tool calls and errors
5. **Testing**: Write comprehensive tests
6. **Monitoring**: Track metrics and health
7. **Security**: Always authenticate and validate input

## Troubleshooting

### Server not starting

```bash
# Check if port is available
lsof -i :50080

# Check logs
tail -f /var/log/mcp-server.log

# Test configuration
python server.py --validate-config
```

### Connection refused

```bash
# Test connectivity
curl http://localhost:50080/health

# Check firewall
sudo ufw status

# Verify server is running
ps aux | grep server.py
```

### Tool call failing

```python
# Enable debug logging
server.set_log_level("DEBUG")

# Test tool directly
result = await server.handle_tool(test_param)
print(result)
```

## Next Steps

- Review [Vault MCP documentation](vault/README.md)
- Learn [agent integration](../docs/AGENT-INTEGRATION.md)
- Try [deployment scripts](../scripts/)
- Join community discussions
