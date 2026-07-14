# Rune-Admin

**Infrastructure & Team Management for Rune-console**

Deploy and manage Rune-console infrastructure for your team. This repository contains deployment automation and team onboarding tools for administrators.

## What is Rune-Admin?

Rune-Admin provides **infrastructure management** for Rune-console:

- **Deployment**: Automated Vault deployment to OCI, AWS, or GCP
- **Key Management**: FHE encryption key generation and secure storage
- **Team Onboarding**: Per-user token issuance and credential distribution
- **Audit Logging**: Structured JSON audit logs for all gRPC operations

For system architecture and data flow details, see [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Prerequisites

### Platform

- **macOS** or **Linux** (Windows is not supported — `runeconsole` registers a systemd or launchd service)

### For Administrators

1. **Runespace account** at [https://runespace.example.com](https://runespace.example.com) — Cluster Endpoint and API Key
2. **Cloud provider account** (AWS, GCP, or OCI) — only needed for cloud deployment

The [installer](#quick-start) auto-checks for the tools it needs (`terraform` and the relevant cloud CLI when targeting a CSP).

### For Team Members

Team members install [Rune](https://github.com/CryptoLabInc/rune) from Claude Marketplace and configure it with:
- Vault Endpoint (provided by admin)
- Vault Token (provided by admin)
- Runespace Cluster Endpoint (provided by admin)
- Runespace API Key (provided by admin)

## Quick Start

### 1. Install Rune-console

The interactive installer downloads the `runeconsole` binary, verifies its
`SHA256SUMS` checksum, renders `runeconsole.conf`, generates TLS certs,
and registers a `runeconsole` service (systemd on Linux, launchd on macOS):

```bash
# Local install
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-console/main/install.sh \
  | sudo bash -s -- --target local

# Cloud install (provisions a VM + bootstraps it via Terraform)
curl -fsSL https://raw.githubusercontent.com/CryptoLabInc/rune-console/main/install.sh \
  | sudo bash -s -- --target aws    # or gcp, oci
```

The installer prompts for team name, Runespace endpoint, and CSP-specific
inputs (region, GCP project ID, OCI compartment OCID). Use `--non-interactive`
plus the `RUNECONSOLE_*` env vars listed in [`install.sh`](install.sh) for CI.

If you'd rather inspect the script before running it, download `install.sh`
and the `SHA256SUMS` file from the release page first, then run `install.sh`
with the binary it pulls down — see [Release Checksum Verification](#release-checksum-verification).

### 2. Verify Deployment

```bash
# gRPC health check (requires grpcurl: brew install grpcurl)
grpcurl -cacert /opt/runeconsole/certs/ca.pem <your-vault-host>:50051 grpc.health.v1.Health/Check

# Expected: { "status": "SERVING" }

# Or use the runeconsole CLI to query daemon status via the admin socket
runeconsole status
```

### 3. Onboard Team Members

```bash
# Issue a per-user token (90-day expiry)
sudo runeconsole token issue --user alice --role member --expires 90d

# Share via secure channel (1Password, Signal, etc.):
#   - Vault Endpoint
#   - Vault Token
#   - Runespace Cluster Endpoint
#   - Runespace API Key
```

Members of the `runeconsole` group can run the CLI without `sudo`.

Team members install [Rune](https://github.com/CryptoLabInc/rune) and configure with the provided credentials.

### From Source (development)

```bash
git clone https://github.com/CryptoLabInc/rune-console.git
cd rune-console
mise install            # Go 1.26, buf, terraform, cloud CLIs
mise run setup          # Resolve Go modules + generate proto stubs
mise run go:build       # Builds runeconsole/bin/runeconsole
# Copy + edit a dev config (the runeconsole/dev/ tree is gitignored):
cp runeconsole/internal/server/testdata/runeconsole.conf.example runeconsole/dev/runeconsole.conf
mise run dev            # Run the daemon in the foreground (uses runeconsole/dev/runeconsole.conf)
```

## Admin Workflows

All admin commands talk to the daemon over a Unix domain socket
(`/opt/runeconsole/admin.sock`). Members of the `runeconsole` group can run
them without `sudo`.

### Manage Tokens

```bash
runeconsole token issue   --user alice --role member --expires 90d
runeconsole token list
runeconsole token rotate  --user alice         # or --all
runeconsole token revoke  --user alice
```

### Manage Roles

```bash
runeconsole role list
runeconsole role create --name <name> --scope a,b,c --top-k 10 --rate-limit 30/60s
runeconsole role update --name <name> [--scope ...] [--top-k ...] [--rate-limit ...]
runeconsole role delete --name <name>
```

### Daemon Health & Logs

```bash
runeconsole status                              # health + socket liveness
runeconsole logs                                # tail audit log
sudo systemctl restart runeconsole              # Linux
sudo launchctl kickstart -k system/com.cryptolabinc.runeconsole   # macOS
```

## Security

### Token Management

- Issue per-user tokens via `runeconsole token issue`
- Share tokens only via encrypted channels (1Password, Signal)
- Never hardcode tokens in code or commit to git
- Rotate periodically via `runeconsole token rotate`

### TLS Requirement

Vault communications MUST use TLS. The installer automatically configures TLS certificates. Without TLS, tokens are exposed to MITM attacks.

### Key Isolation

- **Secret key**: Never leaves Vault VM (architectural constraint)
- **EncKey/EvalKey**: Safe to distribute (public keys)
- Per-agent metadata encryption uses HKDF-derived DEKs (no separate key file)

### Release Checksum Verification

Every GitHub release ships a `SHA256SUMS` file alongside the binaries.
`install.sh` downloads it and runs `sha256sum --check` automatically. To
verify by hand:

```bash
sha256sum --check --ignore-missing SHA256SUMS
```

Trust in the `SHA256SUMS` file itself relies on GitHub's HTTPS download
of the release page.

## Deployment Targets

`install.sh --target <provider>` provisions a VM via Terraform and
bootstraps `runeconsole` on it end-to-end. Each target lives under
`deployment/`:

- **AWS** (Amazon Web Services): `deployment/aws/`
- **GCP** (Google Cloud Platform): `deployment/gcp/`
- **OCI** (Oracle Cloud Infrastructure): `deployment/oci/`

Service files for native installs are under `deployment/systemd/` and
`deployment/launchd/`.

## Uninstall

```bash
# Local: stops the service and removes /opt/runeconsole (prompts to keep data)
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
grpcurl -cacert /opt/runeconsole/certs/ca.pem <vault-host>:50051 grpc.health.v1.Health/Check

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
sudo tail -20 /opt/runeconsole/logs/audit.log
# Or via the CLI:
runeconsole logs
```

### Issue: Vault crashed

```bash
# Inspect logs
sudo journalctl -u runeconsole -n 100        # Linux
sudo log show --predicate 'process == "runeconsole"' --last 10m   # macOS

# Restart
sudo systemctl restart runeconsole           # Linux
sudo launchctl kickstart -k system/com.cryptolabinc.runeconsole   # macOS

# If persistent, re-provision the VM:
sudo bash install.sh --uninstall --target <csp> --install-dir "$HOME/rune-vault-<csp>"
sudo bash install.sh --target <csp>
```

## Documentation

- **Architecture**: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- **Contributing**: [CONTRIBUTING.md](CONTRIBUTING.md)
- **Changelog**: [CHANGELOG.md](CHANGELOG.md)

## Support

- **Issues**: https://github.com/CryptoLabInc/rune-console/issues
- **Discussions**: https://github.com/CryptoLabInc/rune-console/discussions
- **Email**: zotanika@cryptolab.co.kr

## Related Repositories

- **[Rune](https://github.com/CryptoLabInc/rune)**: Claude plugin for organizational memory (what team members install)

## License

Apache License 2.0 - see [LICENSE](LICENSE) for details.
