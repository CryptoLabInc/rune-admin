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

2. **Deploy a Rune-Vault** (see Quick Start below)
   - Vault manages FHE encryption keys for your team
   - One Vault per team (not per developer)

## Quick Start

### 1. Sign up for enVector Cloud

```bash
# Visit https://envector.io and create an account
# Obtain your credentials:
# - Organization ID: your-org-id
# - API Key: envector_xxx

export ENVECTOR_ORG_ID="your-org-id"
export ENVECTOR_API_KEY="envector_xxx"
```

### 2. Install Rune

**Interactive Installation:**

```bash
# Clone Rune
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Run interactive installer
./install.sh        # macOS/Linux
install.bat         # Windows
```

The installer will ask:
- **Team Admin** (deploys infrastructure): Installs Python dependencies for Vault deployment
- **Team Member** (joins existing team): No installation needed, waits for admin package

**What gets installed (Admin only):**
- Python virtual environment
- Dependencies: `pyenvector`, `fastmcp`, `psutil`, `prometheus-client`

**Agent Support:**
- ✅ **Claude Code / Claude Desktop** (Anthropic)
- ✅ **Gemini** (Google)
- ✅ **GitHub Codex** (OpenAI)
- ✅ **Custom agents** (via MCP protocol)

### 3. Deploy Rune-Vault (Team-Shared)

```bash
# Option A: Deploy to Cloud (Recommended)
cd deployment/oci    # or aws, gcp

# Edit terraform.tfvars with your settings
terraform init
terraform plan
terraform apply

# Note the Vault URL from outputs
export VAULT_URL="https://vault-your-team.oci.envector.io"
export VAULT_TOKEN="evt_xxx"

# Option B: Local Testing
cd mcp/vault
./run_vault.sh
# Vault runs at http://localhost:8000
```

**Team Members:** Your admin will share the Vault URL and token with you.

### 4. Onboard Team Members (Administrators)

Generate setup packages for team members:

```bash
# Add a team member
./scripts/add-team-member.sh alice

# This creates: team-setup-alice.zip with:
# - team-specific config
# - setup script
# - Vault connection info
# - enVector credentials

# Share the zip file with Alice
# Alice runs the setup script and is ready to use Rune
```

### 5. Configure Your Agent (Team Members)

After receiving your setup package from admin:

```bash
# Extract package
unzip team-setup-alice.zip
cd team-setup-alice

# Run setup script
./setup.sh    # macOS/Linux
# or
setup.bat     # Windows

# Configure your agent (Claude/Gemini/etc.)
# The script will guide you through agent-specific configuration
```

**Supported Agents:**
- ✅ **Claude Desktop / Claude Code** (Anthropic)
- ✅ **Gemini** (Google)  
- ✅ **GitHub Codex** (OpenAI)
- ✅ **Custom agents** (via MCP protocol)

That's it! Your agent now has access to organizational memory.

## Architecture

```
                    ┌─────────────────────────────┐
                    │     enVector Cloud          │
                    │   (Sign up required)        │
                    │  • Stores encrypted vectors │
                    │  • FHE search               │
                    └──────────┬──────────────────┘
                               │ encrypted data only
                               │
          ┌────────────────────┼────────────────────┐
          │                    │                    │
    ┌─────▼─────┐       ┌──────▼──────┐     ┌──────▼──────┐
    │ envector- │       │  envector-  │     │  envector-  │
    │ mcp-server│       │ mcp-server  │ ... │ mcp-server  │
    │           │       │             │     │             │
    │ • Encrypt │       │ (Scalable)  │     │             │
    │ • Search  │       │             │     │             │
    │ • EncKey  │       └─────────────┘     └─────────────┘
    └─────┬─────┘
          │
          │ decrypt results only
          │
    ┌─────▼──────────────┐
    │   Rune-Vault       │
    │ (Single instance)  │
    │ • Holds SecKey     │
    │ • Decrypt only     │
    │ • One per team     │
    └─────┬──────────────┘
          │
          │ MCP protocol
          │
    ┌─────┴──────┬──────────┬──────────┐
    │            │          │          │
┌───▼───┐  ┌────▼────┐ ┌───▼────┐ ┌───▼────┐
│Claude │  │ Gemini  │ │ Codex  │ │ Custom │
│       │  │         │ │        │ │  Agent │
│Scribe │  │ Scribe  │ │ Scribe │ │        │
└───────┘  └─────────┘ └────────┘ └────────┘
```

**Data Flow:**
1. **Capture**: Agent (Scribe) → envector-mcp-server → encrypt with EncKey
2. **Store**: Encrypted vector → enVector Cloud
3. **Search**: Agent query → envector-mcp-server → encrypted search → Cloud
4. **Decrypt**: Encrypted results → Rune-Vault (SecKey) → plaintext → Agent

**Key Insight:**
- ✅ **Encryption is scalable**: Multiple envector-mcp-servers use public EncKey
- ✅ **Decryption is secure**: Single Rune-Vault holds secret SecKey
- ✅ **Team collaboration**: Same Vault = same keys = shared context
- ✅ **Agent agnostic**: Any agent can use MCP protocol

## Security Architecture

### Two-Tier Key Management

**Why separate encryption and decryption?**

Traditional approach (single Vault):
```
❌ Problem: Vault does everything
   • Encrypt vectors (high volume)
   • Decrypt results (high volume)
   • Holds all keys (security critical)
   • Single bottleneck
```

Rune approach (two-tier):
```
✅ Solution: Separation of concerns

Tier 1: envector-mcp-server (Encryption)
   • Keys: EncKey (public), EvalKey (FHE operations)
   • Operations: Encrypt vectors, FHE search
   • Scaling: Horizontal (spin up more instances)
   • Security: Cannot decrypt (no SecKey)

Tier 2: Rune-Vault (Decryption)
   • Keys: SecKey (secret, never exposed)
   • Operations: Decrypt results only
   • Scaling: Vertical (single instance, high security)
   • Security: Keys in TEE, encrypted at rest
```

**Security Benefits:**
- 🔐 **SecKey isolation**: Only Vault has access, agents cannot extract
- 📈 **Scalable encryption**: envector-mcp-servers scale with load
- 🛡️ **Reduced attack surface**: SecKey in one hardened location
- 🔍 **Audit-friendly**: All decryption in single audit point

**EncKey Compromise?**
- Attacker can encrypt new vectors (spam injection)
- **Cannot read existing data** (no SecKey)
- Mitigation: Authentication on envector-mcp-server (API keys)

**SecKey Compromise?**
- Catastrophic: All data readable
- **Prevention**: TEE deployment, encrypted at rest, strict access control
- **Detection**: Audit logging, anomaly detection

### Key Backup and Recovery

**SecKey backup strategy:**
```bash
# Master key encrypts SecKey
openssl enc -aes-256-cbc -in SecKey.json -out SecKey.enc -pass file:master.key

# Store in multiple locations
# 1. Primary Vault: Active use
# 2. Backup Vault: Hot standby
# 3. Cold storage: Encrypted backup (S3, etc.)
```

**Recovery process:**
1. Detect Vault failure (health check)
2. Promote standby Vault (< 30s)
3. Load SecKey from encrypted backup
4. Resume decryption operations

See [docs/SECURITY.md](docs/SECURITY.md) for threat model.

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
