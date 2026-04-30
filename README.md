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

- **macOS** or **Linux** (Windows is not supported — `runevault` registers a systemd or launchd service)

### For Administrators

1. **enVector Cloud account** at [https://envector.io](https://envector.io) — Cluster Endpoint and API Key
2. **Cloud provider account** (AWS, GCP, or OCI) — only needed for cloud deployment

The [installer](#quick-start) auto-checks for the tools it needs (`cosign` for signature verification, plus `terraform` and the relevant cloud CLI when targeting a CSP).

### For Team Members

Team members install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace and configure it with:
- Vault Endpoint (provided by admin)
- Vault Token (provided by admin)
- enVector Cluster Endpoint (provided by admin)
- enVector API Key (provided by admin)

## Quick Start

### 1. Install Rune-Vault

The interactive installer downloads a Sigstore-signed `runevault` binary,
verifies the signature, renders `runevault.conf`, generates TLS certs,
and registers a `runevault` service (systemd on Linux, launchd on macOS):

```bash
# Local install
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-admin/main/install.sh \
  | sudo bash -s -- --target local

# Cloud install (provisions a VM + bootstraps it via Terraform)
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-admin/main/install.sh \
  | sudo bash -s -- --target aws    # or gcp, oci
```

The installer prompts for team name, enVector endpoint, and CSP-specific
inputs (region, GCP project ID, OCI compartment OCID). Use `--non-interactive`
plus the `RUNEVAULT_*` env vars listed in [`install.sh`](install.sh) for CI.

For tighter supply-chain assurance, download `install.sh` first and verify
its signature before running it — see [Release Signature Verification](#release-signature-verification).

### 2. Verify Deployment

```bash
# gRPC health check (requires grpcurl: brew install grpcurl)
grpcurl -cacert /opt/runevault/certs/ca.pem <your-vault-host>:50051 grpc.health.v1.Health/Check

# Expected: { "status": "SERVING" }

# Or use the runevault CLI to query daemon status via the admin socket
runevault status
```

### 3. Onboard Team Members

```bash
# Issue a per-user token (90-day expiry)
sudo runevault token issue --user alice --role member --expires 90d

# Share via secure channel (1Password, Signal, etc.):
#   - Vault Endpoint
#   - Vault Token
#   - enVector Cluster Endpoint
#   - enVector API Key
```

Members of the `runevault` group can run the CLI without `sudo`.

Team members install [Rune](https://github.com/CryptoLabInc/rune) and configure with the provided credentials.

### From Source (development)

```bash
git clone https://github.com/CryptoLabInc/rune-admin.git
cd rune-admin
mise install            # Go 1.25, buf, terraform, cloud CLIs
mise run setup          # Resolve Go modules + generate proto stubs
mise run go:build       # Builds vault/bin/runevault
# Copy + edit a dev config (the vault/dev/ tree is gitignored):
cp vault/internal/server/testdata/runevault.conf.example vault/dev/runevault.conf
mise run dev            # Run the daemon in the foreground (uses vault/dev/runevault.conf)
```

## Admin Workflows

All admin commands talk to the daemon over a Unix domain socket
(`/opt/runevault/admin.sock`). Members of the `runevault` group can run
them without `sudo`.

### Manage Tokens

```bash
runevault token issue   --user alice --role member --expires 90d
runevault token list
runevault token rotate  --user alice         # or --all
runevault token revoke  --user alice
```

### Manage Roles

```bash
runevault role list
runevault role create --name <name> --scope a,b,c --top-k 10 --rate-limit 30/60s
runevault role update --name <name> [--scope ...] [--top-k ...] [--rate-limit ...]
runevault role delete --name <name>
```

### Daemon Health & Logs

```bash
runevault status                              # health + socket liveness
runevault logs                                # tail audit log
sudo systemctl restart runevault              # Linux
sudo launchctl kickstart -k system/com.cryptolabinc.runevault   # macOS
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

### Release Signature Verification

Every GitHub release ships `SHA256SUMS`, `SHA256SUMS.sig`, and `SHA256SUMS.pem`.
The checksum file is signed keylessly via [Sigstore](https://sigstore.dev) from the
`release.yaml` workflow. Verify before installing:

```bash
cosign verify-blob \
  --signature SHA256SUMS.sig \
  --certificate SHA256SUMS.pem \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp "^https://github.com/CryptoLabInc/rune-admin/.github/workflows/release.yaml@" \
  SHA256SUMS

sha256sum --check --ignore-missing SHA256SUMS
```

The `--certificate-oidc-issuer` and `--certificate-identity-regexp` flags are
required to pin the signature to this repository's workflow — without them,
any Fulcio-issued certificate (including one from a different repository) would
pass verification.

## Deployment Targets

`install.sh --target <provider>` provisions a VM via Terraform and
bootstraps `runevault` on it end-to-end. Each target lives under
`deployment/`:

- **AWS** (Amazon Web Services): `deployment/aws/`
- **GCP** (Google Cloud Platform): `deployment/gcp/`
- **OCI** (Oracle Cloud Infrastructure): `deployment/oci/`

Service files for native installs are under `deployment/systemd/` and
`deployment/launchd/`.

## Uninstall

```bash
# Local: stops the service and removes /opt/runevault (prompts to keep data)
sudo bash install.sh --uninstall --target local

# Cloud: runs `terraform destroy` against the install dir created earlier
sudo bash install.sh --uninstall --target aws \
  --install-dir "$HOME/rune-vault-aws"
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, commands, and guidelines.

## Troubleshooting

### Issue: Team member can't connect

```bash
# Check Vault is reachable
grpcurl -cacert /opt/runevault/certs/ca.pem <vault-host>:50051 grpc.health.v1.Health/Check

# Inspect the security group / firewall rule (port 50051 must be open)
cd "$HOME/rune-vault-<csp>"
terraform show | grep -A5 -E 'security_(group|list)'

# Verify the token — have the team member re-enter it carefully
```

### Issue: Slow decryption

```bash
# Check Vault CPU usage — re-provision with a larger VM if >80%
ssh ubuntu@<vault-host>     # or ec2-user@... / opc@... depending on CSP
top

# Tail audit log for latency
sudo tail -20 /opt/runevault/logs/audit.log
# Or via the CLI:
runevault logs
```

### Issue: Vault crashed

```bash
# Inspect logs
sudo journalctl -u runevault -n 100        # Linux
sudo log show --predicate 'process == "runevault"' --last 10m   # macOS

# Restart
sudo systemctl restart runevault           # Linux
sudo launchctl kickstart -k system/com.cryptolabinc.runevault   # macOS

# If persistent, re-provision the VM:
sudo bash install.sh --uninstall --target <csp> --install-dir "$HOME/rune-vault-<csp>"
sudo bash install.sh --target <csp>
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
