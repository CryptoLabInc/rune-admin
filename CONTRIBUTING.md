# Contributing to Rune-Admin

Thank you for your interest in contributing to Rune-Admin! This document provides guidelines for contributing to the infrastructure and deployment tooling.

## Code of Conduct

Be respectful, collaborative, and constructive in all interactions.

## How to Contribute

### Reporting Issues

Before creating an issue:
1. Check if the issue already exists
2. Collect relevant information:
   - Rune-Admin version
   - Deployment platform (OCI/AWS/GCP)
   - Python version
   - Error messages and logs
   - Steps to reproduce

Create issue at: https://github.com/CryptoLabInc/rune-admin/issues

### Suggesting Features

Feature requests should include:
- **Use case**: What problem does this solve?
- **Proposed solution**: How should it work?
- **Alternatives considered**: What other approaches did you consider?
- **Impact**: Who benefits from this feature (administrators, deployment automation)?

## Development Setup

### Prerequisites

- Python 3.10+
- Docker (for testing deployments)
- Access to cloud provider (OCI/AWS/GCP) for integration testing

### Local Setup

1. **Clone repository**
   ```bash
   git clone https://github.com/CryptoLabInc/rune-admin.git
   cd rune-admin
   ```

2. **Install dependencies**
   ```bash
   pip install -r requirements.txt
   cd tests && pip install -r requirements.txt
   ```

3. **Run tests**
   ```bash
   cd tests
   pytest unit/ -v
   pytest integration/ -v
   ```

## Testing

### Test Structure

```
tests/
├── unit/               # Unit tests for core functionality
│   ├── test_auth.py    # Token validation
│   ├── test_crypto.py  # FHE key generation and encryption
│   ├── test_public_key.py  # Public key bundle
│   └── test_decrypt_scores.py  # Decryption and Top-K
├── integration/        # Integration tests
│   └── test_vault_api.py  # End-to-end Vault API
└── load/              # Load testing
    └── load_test.py
```

### Running Tests

```bash
# All unit tests
pytest tests/unit/ -v

# Specific test file
pytest tests/unit/test_crypto.py -v

# With coverage
pytest tests/unit/ --cov=mcp/vault --cov-report=html

# Integration tests (requires Vault setup)
pytest tests/integration/ -v
```

### Test Requirements

- Unit tests should be fast (< 2s per test)
- Use fixtures for crypto setup to avoid repeated key generation
- Mock external dependencies
- Test both success and error paths

## Code Style

### Python

- Follow PEP 8
- Use type hints where appropriate
- Document functions with docstrings
- Keep functions focused and testable

Example:
```python
def validate_token(token: str) -> None:
    """
    Validates authentication token.
    
    Args:
        token: Authentication token from admin
        
    Raises:
        ValueError: If token is invalid or empty
    """
    if not token or token.strip() != token:
        raise ValueError(f"Access Denied: Invalid Token")
```

### Shell Scripts

- Use `#!/usr/bin/env bash` for portability
- Include error handling (`set -e`)
- Add comments for complex logic
- Test on multiple platforms (macOS, Linux)

## Documentation

### Files to Update

- **README.md**: Quick start guide for administrators
- **docs/ARCHITECTURE.md**: Infrastructure architecture details
- **docs/TEAM-SETUP.md**: Team onboarding procedures
- **CHANGELOG.md**: Version changes

### Documentation Standards

- Use clear, concise language
- Include code examples where helpful
- Keep diagrams up to date (ASCII art for architecture)
- Test all commands before documenting

## Deployment Testing

### Local Testing

```bash
# Test MCP server locally
cd mcp/vault
python vault_mcp.py

# In another terminal, test endpoints
curl http://localhost:50080/health
```

### Platform Testing

Test deployment on each supported platform:

- **OCI**: `deployment/oci/deploy.sh`
- **AWS**: `deployment/aws/deploy.sh`
- **GCP**: `deployment/gcp/deploy.sh`

## Submitting Changes

### Pull Request Process

1. **Create feature branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. **Make changes**
   - Write code
   - Add/update tests
   - Update documentation

3. **Test thoroughly**
   ```bash
   pytest tests/ -v
   ./scripts/check-infrastructure.sh
   ```

4. **Commit with clear message**
   ```bash
   git commit -m "feat: Add monitoring dashboard support
   
   - Add Prometheus metrics endpoint
   - Update deployment scripts
   - Add monitoring documentation"
   ```

5. **Push and create PR**
   ```bash
   git push origin feature/your-feature-name
   # Create PR on GitHub
   ```

### Commit Message Format

```
<type>: <subject>

<body>

<footer>
```

**Types:**
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `test`: Adding or updating tests
- `refactor`: Code change that neither fixes a bug nor adds a feature
- `chore`: Changes to build process or auxiliary tools

**Example:**
```
feat: Add Kubernetes deployment support

- Add k8s manifests for Vault deployment
- Update install scripts to detect k8s
- Add documentation for k8s deployment

Closes #123
```

## Release Process

1. Update version in relevant files
2. Update CHANGELOG.md with all changes
3. Test deployment on all supported platforms
4. Create git tag: `git tag -a v0.3.0 -m "Release v0.3.0"`
5. Push tag: `git push origin v0.3.0`

## Repository Structure

```
rune-admin/
├── mcp/
│   └── vault/              # Vault MCP server
│       ├── vault_mcp.py    # Main server code
│       ├── demo_local.py   # Local testing demo
│       └── verify_crypto_flow.py  # Crypto verification
├── deployment/
│   ├── oci/               # Oracle Cloud deployment
│   ├── aws/               # AWS deployment
│   └── gcp/               # GCP deployment
├── tests/                 # Test suite
├── scripts/               # Deployment and management scripts
├── docs/                  # Documentation
├── vault_keys/           # Generated FHE keys (gitignored)
└── config/               # Configuration templates
```

## Key Components

- **vault_mcp.py**: MCP server exposing Vault operations
- **Deployment scripts**: Platform-specific deployment automation
- **Test suite**: Comprehensive testing of all functionality
- **Documentation**: Admin guides and architecture details

## Security Considerations

- Never commit private keys (SecKey.json)
- Store tokens securely
- Use environment variables for sensitive config
- Review security implications of changes
- Test authentication and authorization

## Getting Help

- **Issues**: GitHub Issues for bugs and features
- **Discussions**: GitHub Discussions for questions
- **Documentation**: Check docs/ folder first

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.

Thank you for contributing to Rune-Admin!
