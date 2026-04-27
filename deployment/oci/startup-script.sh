#!/bin/bash
set -euo pipefail
exec > /var/log/runevault-install.log 2>&1
echo "=== runevault startup at $(date) ==="

for i in $(seq 1 30); do
  apt-get update -q \
    && apt-get -y --fix-broken install \
    && apt-get install -y ca-certificates curl openssl \
    && break
  echo "apt retry $i..." && sleep 10
done

arch=$(dpkg --print-architecture); [ "$arch" = "amd64" ] && carch=amd64 || carch=arm64
curl -fsSL "https://github.com/sigstore/cosign/releases/latest/download/cosign-linux-$${carch}" -o /usr/local/bin/cosign
chmod 0755 /usr/local/bin/cosign

cat > /etc/profile.d/runevault-installer-env.sh <<'ENVFILE'
export RUNEVAULT_TEAM_NAME='${team_name}'
export RUNEVAULT_ENVECTOR_ENDPOINT='${envector_endpoint}'
export RUNEVAULT_ENVECTOR_API_KEY='${envector_api_key}'
ENVFILE
chmod 600 /etc/profile.d/runevault-installer-env.sh
set -a; . /etc/profile.d/runevault-installer-env.sh; set +a

INSTALL_URL="https://raw.githubusercontent.com/CryptoLabInc/rune-admin/${runevault_version}/install.sh"
for i in 1 2 3 4 5; do
  curl -fsSL --retry 5 --retry-delay 10 --connect-timeout 15 "$${INSTALL_URL}" -o /tmp/install.sh && break
  sleep $((i*10))
done

bash /tmp/install.sh --target local --non-interactive --version "${runevault_version}"

rm -f /etc/profile.d/runevault-installer-env.sh
echo "=== completed at $(date) ==="
