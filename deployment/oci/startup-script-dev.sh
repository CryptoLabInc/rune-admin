#!/bin/bash
# Dev mode: installs prereqs only. install.sh + binary injected via SCP by install-dev.sh.
set -euo pipefail
exec > /var/log/runeconsole-install.log 2>&1
echo "=== runeconsole dev startup at $(date) ==="

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

touch /var/run/runeconsole-dev-ready
echo "=== prereqs ready at $(date), waiting for install-dev.sh injection ==="
