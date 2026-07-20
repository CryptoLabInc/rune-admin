#!/bin/bash
set -euo pipefail
exec > /var/log/runeconsole-install.log 2>&1
echo "=== runeconsole startup at $(date) ==="

for i in $(seq 1 30); do
  apt-get update -q \
    && apt-get -y --fix-broken install \
    && apt-get install -y ca-certificates curl openssl \
    && break
  echo "apt retry $i..." && sleep 10
done

# Host firewall: OCI's Ubuntu image ships an iptables ruleset whose INPUT chain
# rejects all inbound except SSH, so opening 50051 in the VCN security list is
# not enough — allow gRPC on the host too and persist it across reboots.
iptables -I INPUT -p tcp -m state --state NEW -m tcp --dport 50051 -j ACCEPT
command -v netfilter-persistent >/dev/null 2>&1 && netfilter-persistent save || true

INSTALL_URL="https://raw.githubusercontent.com/CryptoLabInc/rune-console/${runeconsole_version}/install.sh"
for i in 1 2 3 4 5; do
  curl -fsSL --retry 5 --retry-delay 10 --connect-timeout 15 "$${INSTALL_URL}" -o /tmp/install.sh && break
  sleep $((i*10))
done

bash /tmp/install.sh --target local --non-interactive --version "${runeconsole_version}"

usermod -aG runeconsole ubuntu

echo "=== completed at $(date) ==="
