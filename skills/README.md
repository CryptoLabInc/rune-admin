# Skills

This directory contains agent skills that can be loaded by any compatible AI agent.

## Available Skills

### enVector

**Organizational context memory system**

> **Prerequisite**: Sign up for enVector Cloud at [https://envector.io](https://envector.io) before using this skill. enVector Cloud provides the FHE-encrypted vector database required for organizational memory.

Enables agents to:
- Capture significant organizational decisions
- Search encrypted organizational memory
- Retrieve context with full provenance
- Synthesize comprehensive answers

**Location:** `envector/`

**Documentation:** `envector/SKILL.md`

**MCP Tools:** `envector/tools.json` (if applicable)

## Creating Custom Skills

### 1. Skill Directory Structure

```
skills/
  your-skill/
    SKILL.md          # Documentation (required)
    tools.json        # MCP tool definitions (optional)
    config.yaml       # Configuration (optional)
    examples/         # Usage examples
    tests/            # Tests
```

### 2. SKILL.md Format

```markdown
---
name: your-skill
description: Brief description
license: MIT
metadata:
    skill-author: Your Name
    version: 1.0.0
    category: productivity
---

# Your Skill Name

## What It Does

Clear explanation of the skill's purpose.

## How to Use

Practical examples and instructions.

## Configuration

Environment variables, settings, etc.
```

### 3. MCP Tool Definitions (Optional)

If your skill provides tools via MCP:

```json
{
  "tools": [
    {
      "name": "tool_name",
      "description": "What this tool does",
      "parameters": {
        "type": "object",
        "properties": {
          "param1": {
            "type": "string",
            "description": "Parameter description"
          }
        },
        "required": ["param1"]
      }
    }
  ]
}
```

### 4. Agent-Agnostic Design

**DO:**
- ✓ Use standard MCP protocol
- ✓ Document clearly
- ✓ Provide examples for multiple agents
- ✓ Handle errors gracefully
- ✓ Support configuration via environment variables

**DON'T:**
- ✗ Depend on agent-specific features
- ✗ Hardcode paths or configurations
- ✗ Assume specific agent behavior
- ✗ Use proprietary protocols

## Installation

Skills are installed using the Rune installer:

```bash
# Run interactive installer
./install.sh        # macOS/Linux
install.bat         # Windows

# Skills are configured based on your agent setup
```

## Testing Skills

### Manual Testing

```bash
# Test with different agents
./test-skill.sh your-skill --agent claude
./test-skill.sh your-skill --agent gemini
./test-skill.sh your-skill --agent codex
```

### Automated Testing

```python
# tests/test_your_skill.py
import pytest
from rune.testing import SkillTester

def test_skill_loads():
    tester = SkillTester("your-skill")
    assert tester.can_load()

def test_skill_basic_operation():
    tester = SkillTester("your-skill")
    result = tester.execute("basic_command")
    assert result.success
```

## Contributing Skills

We welcome new skills! To contribute:

1. Create skill directory with proper structure
2. Write comprehensive SKILL.md
3. Test with multiple agents
4. Submit pull request

See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidelines.

## Skill Ideas

Some ideas for community-contributed skills:

- **code-search**: Semantic code search with context
- **doc-gen**: Automatic documentation generation
- **test-gen**: Test generation from specifications
- **refactor**: Automated refactoring assistance
- **debug**: Enhanced debugging with context
- **review**: Code review automation
- **deploy**: Deployment workflow assistance
- **monitor**: System monitoring and alerts

## Support

- **Documentation**: See individual skill README
- **Issues**: [GitHub Issues](https://github.com/CryptoLabInc/rune/issues)
- **Discussions**: [GitHub Discussions](https://github.com/CryptoLabInc/rune/discussions)
