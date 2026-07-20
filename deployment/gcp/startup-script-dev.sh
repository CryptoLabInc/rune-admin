#!/bin/bash
# Dev mode: installs prereqs only. install.sh + binary injected via SCP by install-dev.sh.
set -euo pipefail
exec > /var/log/runeconsole-install.log 2>&1
echo "=== runeconsole dev startup at $(date) ==="

for i in $(seq 1 30); do
  apt-get update -q && apt-get install -y ca-certificates curl openssl && break
  echo "apt retry $i..." && sleep 10
done

touch /var/run/runeconsole-dev-ready
echo "=== prereqs ready at $(date), waiting for install-dev.sh injection ==="
