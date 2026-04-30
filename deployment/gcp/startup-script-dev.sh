#!/bin/bash
# Dev mode: installs prereqs only. install.sh + binary injected via SCP by install-dev.sh.
set -euo pipefail
exec > /var/log/runevault-install.log 2>&1
echo "=== runevault dev startup at $(date) ==="

for i in $(seq 1 30); do
  apt-get update -q && apt-get install -y ca-certificates curl openssl && break
  echo "apt retry $i..." && sleep 10
done

arch=$(dpkg --print-architecture); [ "$arch" = "amd64" ] && carch=amd64 || carch=arm64
curl -fsSL "https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-$${carch}" -o /usr/local/bin/cosign
chmod 0755 /usr/local/bin/cosign

echo "=== prereqs ready at $(date), waiting for install-dev.sh injection ==="
