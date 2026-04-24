# Rune-Vault (rune-admin)

Python 3.12 gRPC server for FHE-encrypted organizational memory.
The secret key never leaves this server.

## Setup

See [CONTRIBUTING.md](CONTRIBUTING.md#development-setup) for initial setup.

## Commands

All commands **must** be run via `mise run` to ensure correct tool versions and venv activation.
Do NOT run python, pytest, or ruff directly.

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
| `mise run dev` | Start local Vault via Docker Compose |
| `mise run certs` | Generate self-signed TLS certificates |

## Rules

- English only in code, commit messages, PR descriptions, and issue bodies
- Do not amend commits or force-push unless explicitly instructed
- All public functions need type hints
- New gRPC methods need corresponding unit tests in `tests/unit/`
- Token/auth changes must update `tests/unit/test_auth.py`
- Run `mise run check` before committing

## Security invariants

- Secret key (`vault_keys/`) must never be logged, returned in API responses, or leave the server process
- Admin server binds to `127.0.0.1` only — never expose externally
- Token secrets must come from environment variables, never hardcoded
- TLS is required for all cloud deployments

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
