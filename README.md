# Rune-Admin

**Infrastructure & Team Management for Rune-Vault**

Deploy and manage Rune-Vault infrastructure for your team. This repository contains deployment automation and team onboarding tools for administrators.

## What is Rune-Admin?

Rune-Admin provides **infrastructure management** for Rune-Vault:

- **Deployment**: Automated Vault deployment to OCI, AWS, or GCP
- **Key Management**: FHE encryption key generation and secure storage
- **Team Onboarding**: Per-user token issuance and credential distribution
- **Audit Logging**: Structured JSON audit logs for all gRPC operations

For system architecture and data flow details, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Prerequisites

### Platform

- **macOS** or **Linux** (Windows is not supported — pyenvector requires Unix)

### For Administrators

1. **enVector Cloud account** at [https://envector.io](https://envector.io) — Cluster Endpoint and API Key
2. **Cloud provider account** (OCI, AWS, or GCP) — only needed for cloud deployment

The [installer](#quick-start) will automatically check and install required tools (Docker, Terraform, etc.).

### For Team Members

Team members install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace and configure it with:
- Vault Endpoint (provided by admin)
- Vault Token (provided by admin)
- enVector Cluster Endpoint (provided by admin)
- enVector API Key (provided by admin)

## Quick Start

### 1. Build and run Rune-Vault

Phase 1 of the Go runtime migration ships the daemon as a single binary
`runevault`. The cloud installer (binary distribution + Terraform) lands
in Phase 3 (issue #64); until then, build from source:

```bash
git clone https://github.com/CryptoLabInc/rune-admin.git
cd rune-admin/vault
go build -o /usr/local/bin/runevault ./cmd/runevault
```

Render `runevault.conf` from the template (paths, TLS cert/key, envector
credentials, team secret) and place it at
`/opt/rune-vault/configs/runevault.conf`:

```bash
# Template
cat vault/internal/server/testdata/runevault.conf.example
# (then write your filled-in copy)
sudo install -m 0600 -o "$(id -un)" runevault.conf /opt/rune-vault/configs/runevault.conf
```

Generate self-signed TLS certs (or supply your own) and start the daemon:

```bash
mise run certs                       # writes certs/server.{pem,key}
runevault daemon start               # foreground; Ctrl+C or `runevault daemon stop` to exit
```

### 2. Verify Deployment

```bash
# gRPC health check (requires grpcurl: brew install grpcurl)
grpcurl -cacert ca.pem <your-vault-host>:50051 grpc.health.v1.Health/Check

# Expected: { "status": "SERVING" }
```

### 3. Onboard Team Members

```bash
# Issue a per-user token
runevault token issue --user alice --role member

# Share via secure channel (1Password, Signal, etc.):
#   - Vault Endpoint
#   - Vault Token
#   - enVector Cluster Endpoint
#   - enVector API Key
```

Team members install [Rune](https://github.com/CryptoLabInc/rune) and configure with the provided credentials.

## Admin Workflows

### Rotate Token

```bash
# Rotate a single user's token
runevault token rotate --user alice

# Rotate all tokens
runevault token rotate --all

# Distribute new tokens to team members via secure channel
```

## Security

### Token Management

- Issue per-user tokens via `runevault token issue`
- Share tokens only via encrypted channels (1Password, Signal)
- Never hardcode tokens in code or commit to git
- Rotate periodically via `runevault token rotate`

### TLS Requirement

Vault communications MUST use TLS. The installer automatically configures TLS certificates. Without TLS, tokens are exposed to MITM attacks.

### Key Isolation

- **Secret key**: Never leaves Vault VM (architectural constraint)
- **EncKey/EvalKey**: Safe to distribute (public keys)
- Per-agent metadata encryption uses HKDF-derived DEKs (no separate key file)

## Deployment Targets

Cloud deployment automation (Terraform modules under `deployment/`) is
on hold during the Phase 1 → Phase 3 transition. Deploy the binary
directly to your VM of choice for now.

- **OCI** (Oracle Cloud Infrastructure): `deployment/oci/` (Phase 3)
- **AWS** (Amazon Web Services): `deployment/aws/` (Phase 3)
- **GCP** (Google Cloud Platform): `deployment/gcp/` (Phase 3)

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, commands, and guidelines.

## Troubleshooting

### Issue: Team member can't connect

```bash
# Check Vault is reachable
grpcurl -cacert ca.pem vault-yourteam.oci.envector.io:50051 grpc.health.v1.Health/Check

# Check firewall rules (port 50051 must be open)
cd deployment/oci
terraform state show oci_core_security_list.vault

# Verify token — have team member re-enter carefully
```

### Issue: Slow decryption

```bash
# Check Vault CPU usage — increase instance resources if >80%
ssh admin@vault-yourteam.oci.envector.io
top

# Check audit log for latency (mounted to host ./logs/)
tail -20 /opt/rune/logs/audit.log
```

### Issue: Vault crashed

```bash
# Check logs
docker logs rune-vault --tail 100

# Restart
docker restart rune-vault

# If persistent, redeploy
cd deployment/oci
terraform destroy
terraform apply
```

## Documentation

- **Architecture**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)
- **Changelog**: [CHANGELOG.md](CHANGELOG.md)

## Support

- **Issues**: https://github.com/CryptoLabInc/rune-admin/issues
- **Discussions**: https://github.com/CryptoLabInc/rune-admin/discussions
- **Email**: zotanika@cryptolab.co.kr

## Related Repositories

- **[Rune](https://github.com/CryptoLabInc/rune)**: Claude plugin for organizational memory (what team members install)

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
