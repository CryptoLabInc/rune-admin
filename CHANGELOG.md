# Changelog

All notable changes to Rune-Admin will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.0] - 2026-02-04

### Changed - Infrastructure Focus
- **Repository Split**: Separated admin infrastructure from user-facing components
- **Dimension Upgrade**: Increased FHE dimension from 32 to 1024 for production use
- **Test Suite**: Refactored tests to work with FastMCP decorator pattern

### Added
- Comprehensive test suite (22 unit tests, 7 integration tests)
- Test coverage for authentication, cryptography, public key management
- Dimension-aware crypto fixtures for testing
- FastMCP implementation pattern separation (business logic vs. MCP decorators)

### Removed
- Plugin-specific files (skills/, examples/, agents/)
- Team member onboarding scripts (moved to plugin repository)
- Plugin-focused documentation

### Fixed
- FastMCP decorator testing issue (business logic now testable independently)
- Dimension mismatch in test fixtures
- Memory optimization for FHE key generation tests

## [0.2.0] - 2026-02-02

### Added - Complete Infrastructure with MCP Servers

**Major Update**: Full-featured Vault infrastructure with deployment automation.

#### Core Infrastructure
- **Vault MCP Server** (`mcp/vault/vault_mcp.py`)
  - `get_public_key`: Returns EncKey, EvalKey bundle
  - `decrypt_scores`: Decrypts search results with Top-K filtering
  - Rate limiting (max top_k=10)
  - Token-based authentication
  
- **FHE Cryptography** (pyenvector integration)
  - Automatic key generation on startup
  - 4-key system: EncKey, SecKey, EvalKey, MetadataKey
  - Dimension-aware cipher operations

#### Deployment Support
- **Multi-cloud deployment**:
  - Oracle Cloud Infrastructure (OCI)
  - Amazon Web Services (AWS)
  - Google Cloud Platform (GCP)
  
- **Installation Scripts**:
  - Automated setup: `install.sh` (macOS/Linux)
  - Deployment automation: `scripts/deploy-vault.sh`
  - Local development: `scripts/vault-dev.sh`

#### Security
- Token-based authentication system
- Public/private key separation
- Environment variable configuration
- Secure key storage patterns

#### Documentation
- **README.md**: Administrator quick start guide
- **docs/ARCHITECTURE.md**: Infrastructure architecture and data flow
- **docs/TEAM-SETUP.md**: Team onboarding procedures
- **SKILL.md**: Operational procedures (legacy, to be removed)

#### Testing & Verification
- Demo scripts: `demo_local.py`, `verify_crypto_flow.py`
- Load testing framework
- Health check endpoints

### Infrastructure Components

**Included in Repository**:
- Vault MCP Server implementation
- Deployment automation for OCI/AWS/GCP
- Key generation and management tools
- Monitoring and health check utilities
- Configuration templates

**External Dependencies**:
- pyenvector >= 1.2.0 (FHE library)
- fastmcp >= 2.0.0 (MCP framework)
- uvicorn (ASGI server)

## [0.1.0] - 2026-01-15

### Added - Initial Release (Documentation Only)

- Repository structure
- Basic documentation files
- Deployment planning documents

---

## Version Notes

### Version 0.3.0
- **Focus**: Production-ready infrastructure
- **Breaking**: Repository split requires updating git remotes
- **Upgrade Path**: Update dimension to 1024, regenerate keys

### Version 0.2.0
- **Focus**: Complete Vault infrastructure
- **Breaking**: Requires Python 3.12, ~4GB RAM for key generation
- **Deployment**: Multi-cloud support with automated scripts

### Version 0.1.0
- **Focus**: Initial planning and documentation
