# Rune-Vault (rune-admin)

Single-binary Go gRPC server (`runevault`) for FHE-encrypted organizational
memory. Built on `github.com/CryptoLabInc/envector-go-sdk`. The secret key
never leaves this server.

## Setup

See [CONTRIBUTING.md](CONTRIBUTING.md#development-setup) for initial setup.

## Commands

All commands **must** be run via `mise run` to ensure correct tool versions.
Do NOT run go, gofmt, or buf directly.

| Command | Description |
|---------|-------------|
| `mise run setup` | Bootstrap (Go modules + proto stubs) |
| `mise run check` | All checks: gofmt + go vet + unit tests (race) |
| `mise run go:build` | Build the runevault binary to `vault/bin/runevault` |
| `mise run go:test` | Run all tests including E2E (requires `RUNEVAULT_TEST_BINARY`) |
| `mise run go:test:unit` | Run unit tests only (E2E excluded by build tag) |
| `mise run go:test:e2e` | Run E2E tests against pre-built binary (run `go:build` first) |
| `mise run go:vet` | Run go vet on all Go packages |
| `mise run go:fmt` | Format Go source files |
| `mise run go:fmt:check` | Check Go formatting without modifying |
| `mise run proto:go` | Regenerate Go protobuf/gRPC stubs into `vault/pkg/vaultpb` |
| `mise run dev` | Run runevault daemon in foreground (uses `vault/dev/runevault.conf`) |
| `mise run certs` | Generate self-signed TLS certificates |
| `mise run fixtures:decrypt` | Decrypt test fixtures (requires `FIXTURES_GPG_PASSPHRASE`) |
| `mise run fixtures:encrypt` | Re-encrypt test fixtures |

## Rules

- English only in code, commit messages, PR descriptions, and issue bodies
- Do not amend commits or force-push unless explicitly instructed
- All exported Go identifiers need a doc comment
- New gRPC methods need corresponding unit tests in `vault/internal/server/grpc_test.go`
- Token/auth changes must update `vault/internal/tokens/store_test.go`
- Run `mise run check` before committing

## Security invariants

- Secret key (`vault-keys/<key-id>/SecKey.json`) must never be logged, returned in API responses, or leave the server process
- Admin transport is a Unix domain socket (mode 0600, vault-user owned) — never expose externally
- Token secrets and FHE keys live in `runevault.conf` (mode 0600); secret YAML fields support `*_file` indirection for KMS-backed deployments
- TLS is required for all cloud deployments (`server.grpc.tls.disable: true` is dev-only)

## Worktree setup

After entering a worktree, you MUST run before any work:

```bash
mise trust && mise run setup
```

## PR format

Follow `.github/PULL_REQUEST_TEMPLATE.md` exactly. Fill every section, replace all placeholders.

## Development workflow

- Feature branches: `issue-{N}-{description}`
- All changes via PR to main
- Reference issue numbers in commits: `feat: description (#N)`
- Rebase onto target branch, not merge commits in feature branches
