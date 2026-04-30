# Changelog

All notable changes to Rune-Vault will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### ⚠ BREAKING CHANGES

- **Vault rewritten in Go as the single binary `runevault`** (#61). YAML config (`runevault.conf`) is now the only configuration source — env-var fallback removed, no migration helper, no deprecation banners. Existing Python-based deployments must be reinstalled via `install.sh`.

### Added

- Production installer `install.sh` with `--target local|aws|gcp|oci`, SHA256SUMS checksum verification, systemd/launchd service registration, and `--uninstall` flow
- Dev installer `scripts/install-dev.sh` (structural sibling of `install.sh`) for local/CSP testing without GitHub releases
- CSP provisioning via Terraform for AWS, GCP, and OCI: preflight CLI/auth checks, cloud-init bootstrap, CA-cert SCP polling
- CSP uninstall flow that wraps `terraform destroy`
- `runevault status` (daemon + admin-socket health) and `runevault logs` (audit log tail) subcommands
- `runevault` group lets members run the CLI without `sudo`
- Multi-platform release pipeline (linux/darwin × amd64/arm64) with `SHA256SUMS` checksum manifest
- `EnsureVault` startup hook to activate keys and ensure index on first run

### Changed

- Cloud VM images bumped to Ubuntu 24.04 LTS
- OCI SCP user is now `ubuntu`
- Daemon lifecycle delegated to the OS service manager (systemd / launchd) instead of Docker
- Admin transport: HTTP on `127.0.0.1:8081` → Unix domain socket at `/opt/runevault/admin.sock` (mode 0600)
- Token / role storage: standalone `vault-tokens.yml` / `vault-roles.yml` → fields under `runevault.conf` with `*_file` indirection support

### Removed

- Python sources, `docker-compose.yml`, `Dockerfile`, GHCR-published Docker image
- `pyenvector` runtime dependency
- Env-var configuration fallback (`VAULT_TLS_DISABLE`, `VAULT_TEAM_SECRET`, `VAULT_AUDIT_LOG`, etc.)

## [0.3.0] - 2026-04-07

### ⚠ BREAKING CHANGES

- **Embedding dimension changed from 768 to 1024**: Switched default embedding model from bge-base-en-v1.5 (768d, English-only) to Qwen3-Embedding-0.6B (1024d, multilingual). All existing encrypted indexes are incompatible and must be re-created with the new dimension. There is no automatic migration path.

### Added
- Multilingual embedding support (100+ languages) via Qwen3-Embedding-0.6B (#53)

### Changed
- `EMBEDDING_DIM` default: 768 → 1024 in `vault_core.py`, `docker-compose.yml`, all deployment scripts, and `install.sh` (#53)

### Internal
- Self-hosted CI runner on OCI (2 OCPU / 8 GB, 3 concurrent jobs) with GitHub Actions workflow for automated format, lint, test, and Docker build (#44)
- Fixture-based integration tests for decrypt pipeline (`_decrypt_scores_impl`, `_decrypt_metadata_impl`) (#44)
- GPG-encrypted test fixtures — no enVector Cloud access required in CI (#44)
- CI `docker-publish.yml` switched to self-hosted runner (#44)
- Local dev installer `scripts/install-dev.sh` for offline development and testing (#52)
- Unit tests trimmed to vault logic only — removed pyenvector-specific tests (#44)
- Removed `tests/e2e/`, `tests/load/`, `scripts/load-test.sh` — replaced by fixture-based integration tests (#44)

## [0.2.1] - 2026-03-26

### Added
- Token rotation command: `runevault token rotate --user X` and `--all` (#24)

### Changed
- Replace if/elif routing with regex route table in admin server

### Fixed
- Restore install dir ownership before docker compose up on local deploy

## [0.2.0] - 2026-03-25

### Added
- **Per-user token auth**: RBAC with `TokenStore`, per-user tokens, role-based top_k limits and rate limiting (#18)
- **Admin server**: Internal HTTP API on `127.0.0.1:8081` for token/role CRUD (#18)
- **CLI**: `runevault` command for token and role management (#18)

### Changed
- Replace HMAC DEK derivation with HKDF-SHA256; remove `MetadataKey.json` dependency (#18)
- Rename default role from `agent` to `member` (#18)
- Migrate cloud deployments to per-user token auth (#18)

### Fixed
- Switch Docker build CI runner from self-hosted to ubuntu-latest

## [0.1.3] - 2026-03-20

### Added
- Interactive one-command installer (`install.sh`) with TLS and multi-cloud support (#27)
- GitHub Actions workflow to build and push Docker image to GHCR (#30)
- gRPC reflection enabled

### Fixed
- Replace cloud-init with startup scripts for OCI, GCP, AWS reliability

## [0.1.2] - 2026-03-17

### Added
- TLS enforcement for Vault gRPC server-side (#17)
- Auto-detect public IP and include in certificate SAN

### Changed
- Expose gRPC port directly; remove ngrok Docker service

### Fixed
- Use gosu for privilege drop so mounted TLS certs are readable

## [0.1.1] - 2026-02-28

### Added
- Per-agent metadata DEK derivation

### Changed
- **Directory restructure**: `mcp/vault/` to `vault/` — Vault is a gRPC service, not an MCP server
- **Sole entry point**: `vault_grpc_server.py` with CLI args (`--host`, `--grpc-port`)
- Extract business logic to `vault_core.py`

### Removed
- `vault_mcp.py` (FastMCP wrapper)
- `fastmcp` and `uvicorn` dependencies
- Port 50080 (legacy MCP HTTP endpoint)

## [0.1.0] - 2026-01-15

### Added
- Initial release: repository structure and documentation
