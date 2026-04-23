#!/bin/bash
# Rune-Vault installer.
#
# Phase 1 (issue #61) replaced the Docker-based deployment with a single
# Go binary `runevault`. The full installer (binary distribution + systemd
# unit) lands in Phase 3 (issue #64). Until then, build from source:
#
#   git clone https://github.com/CryptoLabInc/rune-admin.git
#   cd rune-admin/vault
#   go build -o /usr/local/bin/runevault ./cmd/runevault
#
# Then write /opt/rune-vault/configs/runevault.conf (see
# vault/internal/server/testdata/runevault.conf.example for the schema)
# and run:
#
#   runevault daemon start

set -e

cat <<'EOF' >&2
Rune-Vault: no installer available in Phase 1.

The Docker-based installer was removed when the runtime moved to a single
Go binary. The replacement installer ships in Phase 3 (issue #64).

To build from source now:

  git clone https://github.com/CryptoLabInc/rune-admin.git
  cd rune-admin/vault
  go build -o /usr/local/bin/runevault ./cmd/runevault

Configuration template:
  vault/internal/server/testdata/runevault.conf.example

Drop the rendered config at /opt/rune-vault/configs/runevault.conf, then:

  runevault daemon start

EOF
exit 1
