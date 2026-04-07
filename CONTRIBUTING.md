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

- [mise](https://mise.jdx.dev): `curl https://mise.jdx.dev/install.sh | sh`
- [Docker](https://docs.docker.com/get-docker/) (for local Vault and image builds)

**CSP deployment only:**
- Access to cloud provider (OCI/AWS/GCP)
- GitHub CLI (`gh`) with GHCR push access to the CryptoLabInc organization

### Local Setup

1. **Clone repository**
   ```bash
   git clone https://github.com/CryptoLabInc/rune-admin.git
   cd rune-admin
   ```

2. **Install tools and bootstrap**
   ```bash
   mise install        # Install Python 3.12, buf, ruff, terraform, cloud CLIs
   mise run setup      # Create venv, install deps, generate proto stubs
   ```

3. **Verify setup**
   ```bash
   mise run test:unit  # Run unit tests to verify
   ```

4. **(Optional) Activate mise in your shell**
   ```bash
   eval "$(mise activate zsh)"   # or bash
   ```
   This adds mise-managed tools (`terraform`, `oci`, `gcloud`, etc.) to your PATH for the current session.
   To make it permanent, add the line to your `~/.zshrc` (or `~/.bashrc`).

### Commands

All commands **must** be run via `mise run` to ensure correct tool versions and venv activation.

| Command | Description |
|---------|-------------|
| `mise run test` | Unit + integration tests |
| `mise run test:unit` | Unit tests only |
| `mise run test:cov` | Tests with coverage report |
| `mise run lint` | Ruff linter |
| `mise run lint:fix` | Ruff with auto-fix |
| `mise run format` | Ruff formatter |
| `mise run format:check` | Check formatting without modifying |
| `mise run check` | All checks: format + lint + unit tests |
| `mise run proto` | Regenerate protobuf/gRPC stubs |
| `mise run build` | Build Docker image locally |
| `mise run push` | Build and push multi-platform image to GHCR (requires GHCR access) |
| `mise run dev` | Start local Vault via Docker Compose |
| `mise run certs` | Generate self-signed TLS certificates |

## Testing

### Test Structure

```
tests/
├── unit/          # Fast, isolated tests per module
└── integration/   # End-to-end Vault API tests
```

### Running Tests

All test commands **must** be run via `mise run`:

```bash
mise run test         # Unit + integration tests
mise run test:unit    # Unit tests only
mise run test:cov     # Tests with coverage report
```

### Test Requirements

- Unit tests should be fast (< 2s per test)
- Use fixtures for crypto setup to avoid repeated key generation
- Mock external dependencies
- Test both success and error paths
- New gRPC methods need corresponding unit tests in `tests/unit/`
- Token/auth changes must update `tests/unit/test_auth.py`

## Code Style

### Python

- Follow PEP 8
- All public functions need type hints
- Keep functions focused and testable
- Format and lint with ruff: `mise run format` and `mise run lint`
- Run `mise run check` before committing

### Shell Scripts

- Use `#!/usr/bin/env bash` for portability
- Include error handling (`set -e`)
- Add comments for complex logic
- Test on multiple platforms (macOS, Linux)

### General Rules

- English only in code, commit messages, PR descriptions, and issue bodies

## Documentation

### Files to Update

- **README.md**: Quick start guide for administrators
- **docs/ARCHITECTURE.md**: Infrastructure architecture details
- **CHANGELOG.md**: Version changes

### Documentation Standards

- Use clear, concise language
- Include code examples where helpful
- Keep diagrams up to date (ASCII art for architecture)
- Test all commands before documenting

## Deployment Testing

### Local Testing

```bash
mise run dev    # Start local Vault via Docker Compose
mise run build  # Build Docker image locally
```

### Testing the Installer Locally

Use `scripts/install-dev.sh` to test the full installation flow using local working tree files instead of downloading from GitHub.

```bash
sudo bash scripts/install-dev.sh
```

This script behaves identically to `install.sh` but:
- Copies `docker-compose.yml`, TLS scripts, and Terraform configs from the local repo
- Uses a locally built Docker image (`mise run build`) instead of pulling from GHCR
- Requires no network access to GitHub

## Submitting Changes

### Pull Request Process

1. **Create feature branch**
   ```bash
   git checkout -b worktree-issue-{N}-{description}
   ```

2. **Make changes**
   - Write code
   - Add/update tests
   - Update documentation

3. **Test thoroughly**
   ```bash
   mise run check
   ```

4. **Commit with clear message**
   ```bash
   git commit -m "feat: add monitoring dashboard support (#N)"
   ```

5. **Push and create PR**
   ```bash
   git push origin worktree-issue-{N}-{description}
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
├── vault/                  # Rune-Vault gRPC server (see [Architecture](docs/ARCHITECTURE.md))
├── deployment/            # Terraform configs (OCI, AWS, GCP) + monitoring
├── scripts/
├── tests/                 # Unit, integration tests
├── docs/                  # Architecture docs
└── install.sh             # Interactive installer
```

## Vault Architecture

Core server code is in `vault/`. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

## Security Considerations

- Secret key (`vault_keys/`) must never be logged, returned in API responses, or leave the server process
- Admin server binds to `127.0.0.1` only — never expose externally
- Never commit private keys (SecKey.json)
- Token secrets must come from environment variables, never hardcoded
- TLS is required for all cloud deployments
- Review security implications of changes
- Test authentication and authorization

## Getting Help

- **Issues**: GitHub Issues for bugs and features
- **Discussions**: GitHub Discussions for questions
- **Documentation**: Check docs/ folder first

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.

Thank you for contributing to Rune-Admin!
