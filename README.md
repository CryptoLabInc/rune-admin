# Rune

**Agent-Agnostic Organizational Context Memory System**

Build organizational memory that works with any AI agent (Claude, Gemini, Codex, or custom agents). Capture decisions automatically, retrieve them with FHE encryption, never lose institutional knowledge.

## What is Rune?

Rune is an **agent-agnostic framework** for organizational context memory:

- **📝 Capture**: Scribe agents watch your tools (Slack, Notion, GitHub) and identify significant decisions
- **🔐 Encrypt**: Store decisions as FHE-encrypted vectors (searchable but cryptographically private)
- **🔍 Retrieve**: Any agent can search organizational memory and get full context
- **🤝 Share**: Teams automatically share context through encrypted keys (no manual sync)

**Agent Agnostic**: Works with Claude, Gemini, Codex, or any AI agent that can integrate with MCP (Model Context Protocol).

## Prerequisites

Before using Rune, you must:

1. **Sign up for enVector Cloud** at [https://envector.io](https://envector.io)
   - enVector Cloud provides the FHE-encrypted vector database for storing and searching organizational context
   - Create an account and obtain your API credentials (`org-id`, `api-key`)
   - **Note:** enVector Cloud currently provides minimal setup (cluster creation and API key issuance). Multi-tenant support is not yet available.

2. **Deploy a Rune Vault** (see Quick Start below)
   - Vault manages FHE encryption keys for your team
   - One Vault per team (not per developer)

## Quick Start

### 1. Choose Your Agent

Rune works with:
- ✅ **Claude Code / Claude Desktop** (Anthropic)
- ✅ **Gemini** (Google)
- ✅ **GitHub Codex** (OpenAI)
- ✅ **Custom agents** (via MCP protocol)

# Clone Rune
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Install for your agent
./install.sh --agent claude    # For Claude
./install.sh --agent gemini    # For Gemini
./install.sh --agent codex     # For Codex
./install.sh --agent custom    # For custom agents
```

### 3. Deploy Rune Vault (Team-Shared)

```bash
# Option 1: Use managed Vault (recommended for teams)
./scripts/deploy-vault.sh --provider oci --team-name your-team

# Option 2: Self-hosted
docker-compose -f deployment/vault/docker-compose.yml up -d

# Option 3: Local dev (testing only)
./scripts/vault-dev.sh
```

### 4. Configure Your Agent

```bash
# Share these with your team
export VAULT_URL="https://vault-your-team.oci.envector.io"
export VAULT_TOKEN="evt_xxx"

# Each team member runs this once
./scripts/configure-agent.sh
```

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                   enVector Cloud                    │
│   https://envector.io - Sign up required            │
│         Stores encrypted vectors only               │
└─────────────────────────────────────────────────────┘
          ▲               ▲               ▲
          │ encrypted     │ encrypted     │ encrypted
┌─────────┴────┐  ┌───────┴──────┐  ┌────┴─────────┐
│   Claude     │  │    Gemini    │  │    Codex     │
│              │  │              │  │              │
│    Scribe    │  │    Scribe    │  │    Scribe    │
│      ↓       │  │      ↓       │  │      ↓       │
│  MCP Client  │  │  MCP Client  │  │  MCP Client  │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘
       │                 │                 │
       └─────────────────┴─────────────────┘
                         │
                         ▼
        ┌────────────────────────────────────┐
        │     envector-mcp-server(s)         │  ← Scalable
        │  - Encrypts vectors (EncKey)       │
        │  - Handles insert/search           │
        └────────────────┬───────────────────┘
                         │ EncKey, EvalKey
                         ▼
              ┌──────────────────────┐
              │      Rune Vault      │  ← Single instance
              │   - SecKey (decrypt) │
              │   - One per team     │
              └──────────────────────┘
```

**Key Insight:**
- Each team member runs their preferred agent
- **envector-mcp-server** handles encryption (scalable, uses public keys)
- **Rune Vault** handles decryption only (single instance, holds SecKey)
- Context captured by one agent is accessible to all team members
- No manual synchronization required

## Project Structure

```
Rune/
├── README.md                    # This file
├── LICENSE                      # Open source license
├── install.sh                   # Agent-agnostic installer
│
├── skills/                      # Agent skills/tools
│   ├── envector/               # enVector organizational memory skill
│   │   ├── SKILL.md            # Skill documentation
│   │   ├── tools.json          # MCP tool definitions
│   │   └── examples/           # Usage examples
│   └── README.md               # How to create custom skills
│
├── agents/                      # Agent specifications
│   ├── scribe.md               # Context capture agent
│   ├── retriever.md            # Context retrieval agent
│   └── README.md               # Agent integration guide
│
├── mcp/                         # MCP server implementations
│   ├── vault/                  # FHE key management + decryption
│   │   ├── vault_mcp.py
│   │   ├── docker-compose.yml
│   │   └── README.md
│   ├── envector-mcp-server/    # Encryption + search (git submodule)
│   │   ├── srcs/server.py
│   │   ├── MANUAL.md
│   │   └── README.md
│   └── README.md               # MCP integration guide
│
├── deployment/                  # Deployment configurations
│   ├── oci/                    # Oracle Cloud Infrastructure
│   ├── aws/                    # Amazon Web Services
│   ├── gcp/                    # Google Cloud Platform
│   ├── on-premise/             # Self-hosted
│   └── README.md
│
├── scripts/                     # Utility scripts
│   ├── deploy-vault.sh         # Deploy team Vault
│   ├── configure-agent.sh      # Configure agent environment
│   ├── vault-dev.sh            # Local dev Vault
│   └── README.md
│
├── examples/                    # Real-world examples
│   ├── team-collaboration/     # Multi-developer workflow
│   ├── confidential-project/   # Secure project example
│   └── README.md
│
├── docs/                        # Documentation
│   ├── ARCHITECTURE.md         # System architecture
│   ├── SECURITY.md             # Security model
│   ├── AGENT-INTEGRATION.md    # How to integrate new agents
│   ├── TEAM-SETUP.md           # Team collaboration guide
│   └── FAQ.md
│
├── tests/                       # Integration tests
    ├── test_vault.py
    ├── test_agent_integration.py
    └── README.md
```

## Use Cases

### 1. Team Collaboration (Confidential Projects)

**Scenario:** 3 developers building a confidential application.

```bash
# Team admin deploys shared Vault
./scripts/deploy-vault.sh --team confidential-app

# Alice uses Claude
./install.sh --agent claude
# Captures: "We chose FHE approach X for memory efficiency"

# Bob uses Gemini
./install.sh --agent gemini
# Asks: "How should we handle memory?" → Gets Alice's context

# Carol uses Codex
./install.sh --agent codex
# Sees full team decision history automatically
```

### 2. Organizational Memory

Prevent context loss when:
- Key people leave the company
- Decisions need to be revisited
- New team members onboard
- Similar questions arise months later

### 3. Regulated Industries

Healthcare, finance, legal, government:
- HIPAA/PCI-DSS/FedRAMP compliant (FHE encryption)
- Keys never leave your infrastructure
- Audit trail of all context access
- Data sovereignty guaranteed

## Agent Integration

### For Agent Developers

Rune uses **MCP (Model Context Protocol)** for agent integration:

```python
# Example: Integrate your custom agent
from rune import ContextMemory

memory = ContextMemory(
    vault_url="https://vault-your-team.oci.envector.io",
    vault_token="evt_xxx",
    cloud_url="https://api.envector.io"  # Optional
)

# Capture context
memory.capture(
    source="slack",
    content="We chose Postgres for better JSON support",
    metadata={"channel": "#engineering", "author": "alice"}
)

# Retrieve context
results = memory.search("Why did we choose Postgres?")
# Returns: Full decision context with sources
```

See [docs/AGENT-INTEGRATION.md](docs/AGENT-INTEGRATION.md) for details.

## Security Model

**Zero-Trust FHE Architecture:**

1. **Data encrypted at source** (your infrastructure)
2. **Cloud never sees plaintext** (FHE allows search on encrypted data)
3. **Keys never leave Vault** (isolated from agents)
4. **Team shares keys** (same Vault = same encryption)

See [docs/SECURITY.md](docs/SECURITY.md) for threat model and security analysis.

## Roadmap

### Current (v0.1.0)
- ✅ enVector skill for organizational memory
- ✅ Scribe and Retriever agent specs
- ✅ Vault MCP server (demo implementation)
- ✅ Team collaboration support
- ✅ Claude/Gemini/Codex examples

### Next (v0.2.0)
- [ ] Production Vault deployment (OCI/AWS/GCP)
- [ ] JWT authentication (replace hardcoded tokens)
- [ ] Encrypted key storage
- [ ] Observability (metrics, logging, tracing)
- [ ] Integration tests

### Future (v0.3.0+)
- [ ] pyenvector CLI (simplify UX)
- [ ] Advanced capture rules (ML-based)
- [ ] Multi-tenant SaaS mode
- [ ] Additional agent integrations

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

**Areas we need help with:**
- Agent integrations (new agents)
- Deployment scripts (more CSPs)
- Security hardening
- Documentation improvements
- Example workflows

## Community

- **GitHub Issues**: Bug reports and feature requests
- **Discussions**: Questions and community support
- **Discord**: Real-time chat (coming soon)

## License

[MIT License](LICENSE) - Free for commercial and non-commercial use

## Credits

Built by [CryptoLabInc](https://github.com/CryptoLabInc) using:
- [MCP](https://modelcontextprotocol.io) - Model Context Protocol by Anthropic
- Inspired by [claude-mem](https://github.com/cyanheads/claude-mem)

## Support

- **Documentation**: [docs/](docs/)
- **Issues**: [GitHub Issues](https://github.com/CryptoLabInc/rune/issues)
- **Email**: [zotanika@cryptolab.co.kr](mailto:[zotanika@cryptolab.co.kr])
