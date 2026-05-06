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
   - Go version (`go version`)
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

**CSP deployment only:**
- Access to a cloud provider (AWS, GCP, or OCI) and the matching CLI authenticated locally
- Docker (used by `scripts/install-dev.sh` to cross-compile a Linux/amd64 binary for cloud VMs)

### Local Setup

1. **Clone repository**
   ```bash
   git clone https://github.com/CryptoLabInc/rune-admin.git
   cd rune-admin
   ```

2. **Install tools and bootstrap**
   ```bash
   mise install        # Install Go 1.25, buf, terraform, cloud CLIs
   mise run setup      # Resolve Go modules, generate proto stubs
   ```

3. **Verify setup**
   ```bash
   mise run go:test:unit  # Run unit tests to verify
   ```

4. **(Optional) Activate mise in your shell**
   ```bash
   eval "$(mise activate zsh)"   # or bash
   ```
   This adds mise-managed tools (`terraform`, `oci`, `gcloud`, etc.) to your PATH for the current session.
   To make it permanent, add the line to your `~/.zshrc` (or `~/.bashrc`).

### Commands

All commands **must** be run via `mise run` to ensure correct tool versions.

See [CLAUDE.md](CLAUDE.md#commands) (or [AGENTS.md](AGENTS.md#commands)) for the complete task table.

## Testing

### Test Structure

```
vault/internal/
├── tokens/        # Token store + role/rate-limit unit tests
├── crypto/        # HKDF + AES-CTR + envector-go-sdk wrappers
├── server/        # gRPC handlers, interceptors, audit, admin UDS, config
├── commands/      # CLI subcommands + admin client
└── tests/         # E2E (build tag `e2e`): decrypt pipeline (fixture-based) + CLI smoke
```

### Running Tests

```bash
mise run go:test:unit     # Unit tests only (E2E excluded by build tag)
mise run go:build         # Build vault/bin/runevault first…
mise run go:test:e2e      # …then run E2E against the built binary
mise run go:test          # All tests including E2E (requires RUNEVAULT_TEST_BINARY)
```

### Test Fixtures

Integration tests use GPG-encrypted fixtures containing FHE keys and ciphertext blobs. See [tests/FIXTURES.md](tests/FIXTURES.md) for the full update procedure, including passphrase rotation and re-encryption steps. The fixture-based decrypt-pipeline test under `vault/internal/tests/` skips automatically when `tests/fixtures/` is not decrypted.

### Test Requirements

- Unit tests should be fast (< 2s per test)
- Use fixtures for crypto setup to avoid repeated key generation
- Mock external dependencies
- Test both success and error paths
- New gRPC methods need corresponding unit tests in `vault/internal/server/grpc_test.go`
- Token/auth changes must update `vault/internal/tokens/store_test.go`

## Code Style

### Go

- Run `mise run go:fmt` to format
- All exported identifiers need a doc comment
- Tests live alongside the code they test (`*_test.go`)
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
mise run dev         # Run runevault daemon in foreground (uses vault/dev/runevault.conf)
mise run go:build    # Build runevault binary to vault/bin/runevault
```

### Testing the Installer Locally

`scripts/install-dev.sh` is a structural sibling of `install.sh` that
exercises the full install flow against a locally built binary instead
of a published GitHub release.

```bash
# Local install into a rootless prefix (no service registration)
RUNEVAULT_SKIP_SERVICE=1 \
  bash scripts/install-dev.sh --target local --prefix "$HOME/runevault-test"

# Cloud install: cross-compiles linux/amd64 in golang:1.25-bookworm,
# uploads via SCP, and runs install.sh on the remote VM.
bash scripts/install-dev.sh --target oci --install-dir "$HOME/rune-vault-oci"
```

Flags mirror `install.sh`: `--target`, `--install-dir`, `--prefix`,
`--non-interactive`, `--uninstall`, `--force`. Uninstall is delegated to
`install.sh --uninstall`.

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
├── vault/
│   ├── cmd/                       # runevault binary entry point
│   ├── internal/                  # commands, server, tokens, crypto, tests
│   ├── pkg/vaultpb/               # generated gRPC stubs
│   ├── proto/                     # .proto source
│   └── dev/                       # local dev config (gitignored)
├── deployment/
│   ├── aws/  gcp/  oci/           # Terraform per CSP
│   ├── systemd/runevault.service  # Linux service unit
│   └── launchd/com.cryptolabinc.runevault.plist  # macOS service
├── scripts/
│   ├── install-dev.sh             # Dev sibling of install.sh
│   ├── generate-certs.sh          # Self-signed TLS certs for dev
│   └── generate-test-fixtures.py  # Generates GPG-encrypted test fixtures
├── tests/                         # Encrypted fixture archive (see FIXTURES.md)
├── docs/ARCHITECTURE.md           # Architecture & data flow
└── install.sh                     # Production installer (SHA256SUMS-verified)
```

## Vault Architecture

Core server code is in `vault/`. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for details.

## Security Considerations

- Secret key (`vault-keys/<key-id>/SecKey.json`) must never be logged, returned in API responses, or leave the server process
- Admin transport is a Unix domain socket (mode 0600, vault-user owned) — never expose externally
- Never commit private keys (`SecKey.json`) or filled-in `runevault.conf` files
- Token secrets and FHE keys live in `runevault.conf` (mode 0600); secret YAML fields support `*_file` indirection for KMS-backed deployments
- TLS is required for all cloud deployments (`server.grpc.tls.disable: true` is dev-only)
- Review security implications of changes
- Test authentication and authorization

## Getting Help

- **Issues**: GitHub Issues for bugs and features
- **Discussions**: GitHub Discussions for questions
- **Documentation**: Check docs/ folder first

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.

Thank you for contributing to Rune-Admin!
