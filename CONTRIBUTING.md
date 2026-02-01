# Contributing to Rune

Thank you for your interest in contributing to Rune! This document provides guidelines and instructions for contributing.

## Ways to Contribute

### 1. Code Contributions

- **New Skills**: Create agent-agnostic skills
- **Agent Integrations**: Add support for new agents
- **MCP Servers**: Implement new MCP servers
- **Deployment Scripts**: Add support for new cloud providers
- **Bug Fixes**: Fix issues and improve reliability
- **Performance**: Optimize performance and scalability

### 2. Documentation

- **Guides**: Write tutorials and how-to guides
- **Examples**: Add real-world usage examples
- **API Docs**: Improve API documentation
- **Translations**: Translate docs to other languages

### 3. Community

- **Answer Questions**: Help others in discussions
- **Report Bugs**: Submit detailed bug reports
- **Feature Requests**: Suggest new features
- **Testing**: Test new releases and provide feedback

## Getting Started

### 1. Fork and Clone

```bash
# Fork on GitHub
# Then clone your fork
git clone https://github.com/YOUR-USERNAME/rune.git
cd rune

# Add upstream remote
git remote add upstream https://github.com/CryptoLabInc/rune.git
```

### 2. Create Branch

```bash
# Create feature branch
git checkout -b feature/your-feature-name

# Or bug fix branch
git checkout -b fix/issue-number-description
```

### 3. Make Changes

Follow our coding standards (see below) and make your changes.

### 4. Test

```bash
# Run tests
pytest tests/

# Run linter
flake8 .

# Check types
mypy .
```

### 5. Commit

```bash
# Stage changes
git add .

# Commit with descriptive message
git commit -m "Add feature: description of feature"

# Follow conventional commits format:
# feat: new feature
# fix: bug fix
# docs: documentation changes
# test: test changes
# refactor: code refactoring
# chore: maintenance tasks
```

### 6. Push and PR

```bash
# Push to your fork
git push origin feature/your-feature-name

# Create Pull Request on GitHub
```

## Coding Standards

### Python

- **Style**: Follow PEP 8
- **Docstrings**: Use Google style docstrings
- **Type Hints**: Add type hints for all functions
- **Testing**: Write tests for new code

```python
from typing import List, Dict, Optional

def encrypt_vector(
    vector: List[float],
    metadata: Optional[Dict[str, str]] = None
) -> Dict[str, any]:
    """Encrypt vector using FHE keys.
    
    Args:
        vector: Vector to encrypt
        metadata: Optional metadata to attach
        
    Returns:
        Dictionary with encrypted_vector and vector_id
        
    Raises:
        ValueError: If vector is empty
        EncryptionError: If encryption fails
    """
    if not vector:
        raise ValueError("Vector cannot be empty")
    
    # Implementation
    return {"encrypted_vector": [...], "vector_id": "vec_123"}
```

### JavaScript/TypeScript

- **Style**: Use Prettier and ESLint
- **Types**: Use TypeScript for all new code
- **JSDoc**: Add JSDoc comments
- **Testing**: Use Jest for testing

```typescript
/**
 * Encrypt vector using FHE keys
 * 
 * @param vector - Vector to encrypt
 * @param metadata - Optional metadata to attach
 * @returns Encrypted vector and ID
 * @throws {ValueError} If vector is empty
 * @throws {EncryptionError} If encryption fails
 */
export async function encryptVector(
  vector: number[],
  metadata?: Record<string, string>
): Promise<{ encryptedVector: any; vectorId: string }> {
  if (vector.length === 0) {
    throw new ValueError('Vector cannot be empty');
  }
  
  // Implementation
  return { encryptedVector: [...], vectorId: 'vec_123' };
}
```

### Shell Scripts

- **Shebang**: Use `#!/bin/bash`
- **Set options**: `set -e` (exit on error)
- **Comments**: Document complex logic
- **Error handling**: Check command success

```bash
#!/bin/bash
set -e

# Deploy Rune Vault to cloud provider
# Usage: ./deploy-vault.sh --provider oci --team-name myteam

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Validate arguments
if [ -z "$PROVIDER" ]; then
    log_error "Missing required argument: --provider"
fi

# Main logic
log_info "Deploying Vault..."
```

### Documentation

- **Markdown**: Use standard Markdown
- **Code blocks**: Specify language for syntax highlighting
- **Examples**: Provide working examples
- **Links**: Use relative links for internal docs

````markdown
# Feature Name

Brief description of the feature.

## Installation

```bash
./install.sh --feature your-feature
```

## Usage

```python
from rune import YourFeature

feature = YourFeature()
result = feature.do_something()
```

## Configuration

See [configuration guide](docs/CONFIG.md) for details.
````

## Pull Request Process

### 1. PR Title

Use conventional commit format:

```
feat: add support for Gemini agent
fix: resolve vault connection timeout
docs: update team setup guide
test: add integration tests for monitor agent
```

### 2. PR Description

Include:
- **What**: Brief description of changes
- **Why**: Reason for changes
- **How**: Implementation approach
- **Testing**: How you tested
- **Screenshots**: If UI changes

Template:
```markdown
## What
Added support for Gemini agent integration.

## Why
Users requested Gemini support for team collaboration.

## How
- Implemented MCP client for Gemini
- Added configuration templates
- Updated installation script

## Testing
- [x] Unit tests pass
- [x] Integration tests pass
- [x] Manual testing with Gemini agent
- [x] Documentation updated

## Screenshots
N/A
```

### 3. Review Process

- Maintainers will review your PR
- Address feedback promptly
- Keep PR focused (one feature/fix per PR)
- Update based on review comments

### 4. Merge

Once approved:
- Maintainer will merge your PR
- Your contribution will be in the next release
- Thank you! 🎉

## Development Setup

### Prerequisites

- Python 3.11+
- Node.js 18+ (for JavaScript contributions)
- Docker (for local testing)
- Git

### Local Development

```bash
# Clone repository
git clone https://github.com/CryptoLabInc/rune.git
cd rune

# Install Python dependencies
pip install -r requirements.txt
pip install -r requirements-dev.txt

# Install Node dependencies (if working on JS)
npm install

# Start local Vault for testing
./scripts/vault-dev.sh

# Run tests
pytest tests/

# Run linter
flake8 .
black --check .

# Type checking
mypy .
```

### Running Tests

```bash
# Unit tests only
pytest tests/unit/

# Integration tests (requires Vault)
pytest tests/integration/

# Specific test file
pytest tests/test_vault.py

# With coverage
pytest --cov=rune tests/

# Verbose output
pytest -v tests/
```

## Reporting Issues

### Bug Reports

Use the bug report template:

```markdown
**Description**
Clear description of the bug.

**To Reproduce**
Steps to reproduce:
1. Do this
2. Then that
3. See error

**Expected Behavior**
What should happen.

**Actual Behavior**
What actually happened.

**Environment**
- OS: macOS 14.0
- Python: 3.11.5
- Rune: 0.1.0
- Agent: Claude Desktop 1.0

**Logs**
```
Error logs here
```

**Additional Context**
Any other relevant information.
```

### Feature Requests

Use the feature request template:

```markdown
**Problem**
What problem does this solve?

**Proposed Solution**
How should it work?

**Alternatives Considered**
What other approaches did you consider?

**Additional Context**
Any other relevant information.
```

## Community Guidelines

### Code of Conduct

- Be respectful and inclusive
- Welcome newcomers
- Give constructive feedback
- Focus on what's best for the community

### Communication

- **GitHub Issues**: Bug reports, feature requests
- **GitHub Discussions**: Questions, ideas, general discussion
- **Pull Requests**: Code contributions
- **Discord** (coming soon): Real-time chat

## Recognition

Contributors are recognized in:
- CONTRIBUTORS.md file
- Release notes
- Project README

Thank you for contributing to Rune! 🙏
